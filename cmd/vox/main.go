package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"golang.design/x/mainthread"

	"vox/internal/appctx"
	"vox/internal/audio"
	"vox/internal/classify"
	"vox/internal/claude"
	"vox/internal/commands"
	"vox/internal/config"
	"vox/internal/flags"
	"vox/internal/format"
	"vox/internal/hotkey"
	"vox/internal/inject"
	"vox/internal/pipeline"
	"vox/internal/prompt"
	"vox/internal/transcribe"
	"vox/internal/ui"
	"vox/internal/userconfig"
	"vox/internal/vocab"
)

const (
	banner = `
 __   _____ _  __
 \ \ / / _ \ \/ /
  \ V / (_) >  <
   \_/ \___/_/\_\
  voice-activated AI
`
	// Version is the current release identifier shown by "vox version".
	version = "vox 2.0.0-dev (Moonshots XXIII)"
)

// runtimeSettings holds the in-process state for the toggles the user can
// flip from the menubar. atomic.Bool means any goroutine can read or write
// without locking. Persistence is done separately via config.SavePref.
type runtimeSettings struct {
	Paused        atomic.Bool
	HoldToTalk    atomic.Bool
	SoundsEnabled atomic.Bool
	AutoPaste     atomic.Bool
	AIPostProcess atomic.Bool
	PromptMode    atomic.Bool
	VoiceCommands atomic.Bool
	ContextAware  atomic.Bool
}

var settings runtimeSettings

// processMu guards a "stop-and-process" cycle. handleStopAndProcess holds
// it from before its first recorder.Stop() through the end of injection;
// settingsWatcher's pause handler tries to acquire it before stopping the
// recorder. If processMu is already held, a stop-and-process is in flight
// and pause should leave it alone — the audio was captured before the
// pause click, so the user expects that one final transcription to land.
var processMu sync.Mutex

func main() {
	mainthread.Init(run)
}

func run() {
	log.SetFlags(0)

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "setup":
			runSetup()
			return
		case "config":
			runConfig()
			return
		case "help", "--help", "-h":
			runHelp()
			return
		case "version", "--version":
			fmt.Println(version)
			return
		default:
			fmt.Fprintf(os.Stderr, "vox: unknown command %q\nRun 'vox help' for usage.\n", os.Args[1])
			os.Exit(1)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.Load()

	fmt.Print(banner)
	fmt.Printf("Config: %s\n\n", cfg)

	var logger *slog.Logger
	if cfg.Verbose {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	} else {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	}

	// Load user config file (~/.vox/config.yaml).
	userCfg, err := userconfig.Load()
	if err != nil {
		logger.Warn("failed to load user config", "error", err)
	}

	// Initialize LaunchDarkly flags (graceful if no SDK key).
	flagClient, err := flags.Init(userCfg)
	if err != nil {
		logger.Warn("LD flags init", "error", err)
	}
	defer flagClient.Close()

	// Check Accessibility permission.
	if !hotkey.CheckAccessibility() {
		fmt.Fprintln(os.Stderr, "Error: Accessibility permission required.")
		fmt.Fprintln(os.Stderr, "  Grant it in: System Settings > Privacy & Security > Accessibility")
		fmt.Fprintln(os.Stderr, "  Add your terminal app (Terminal, iTerm2, etc.) to the list.")
		os.Exit(1)
	}

	// Check Microphone permission.
	if !hotkey.RequestMicrophoneAccess() {
		fmt.Fprintln(os.Stderr, "Microphone denied — grant it in System Settings > Privacy & Security > Microphone")
		os.Exit(1)
	}

	// Check Whisper server.
	whisperClient := transcribe.NewClient(cfg.WhisperURL)
	if err := whisperClient.HealthCheck(ctx); err != nil {
		fmt.Printf("Warning: Whisper server unavailable at %s (%v)\n", cfg.WhisperURL, err)
		fmt.Println("  Transcription will fail until the server is reachable.")
		fmt.Println()
	} else if cfg.Verbose {
		logger.Debug("whisper health check passed", "url", cfg.WhisperURL)
	}

	// Clean up any orphaned temp files from prior crashes in the background.
	go func() {
		if n := audio.CleanupOrphanedTempFiles(); n > 0 {
			logger.Info("cleaned up orphaned temp files", "count", n)
		}
	}()

	// Create audio recorder.
	recorder, err := audio.NewRecorder()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintln(os.Stderr, "Install sox: brew install sox")
		os.Exit(1)
	}

	// Load custom vocabulary for whisper.cpp hints.
	var initialPrompt string
	if cfg.VocabTerms != "" || cfg.VocabFile != "" {
		initialPrompt, err = vocab.Load(cfg.VocabTerms, cfg.VocabFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load vocabulary: %v\n", err)
		} else if initialPrompt != "" && cfg.Verbose {
			logger.Debug("loaded custom vocabulary", "prompt", initialPrompt)
		}
	}

	// Transcription options used for every request.
	transcribeOpts := transcribe.TranscribeOptions{
		InitialPrompt: initialPrompt,
		Language:      cfg.Language,
	}

	// Initialize Claude API client (nil if no ANTHROPIC_API_KEY).
	var claudeClient *claude.Client
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		claudeClient = claude.NewClient(apiKey, flagClient.AIModel())
		if cfg.Verbose {
			logger.Debug("Claude API client initialized", "model", flagClient.AIModel())
		}
	}

	// Initialize prompt mode executor.
	var promptExec *prompt.Executor
	if claudeClient != nil {
		promptExec = prompt.NewExecutor(claudeClient, inject.ReadClipboard)
	}

	// Initialize voice command registry.
	cmdRegistry := commands.NewRegistry(commands.DefaultCommands()...)

	// Build hotkey labels for display.
	hotkeyLabel := triggerLabel(cfg.Triggers)

	// Seed runtime settings from the loaded config. The UI mirrors these
	// initial values; subsequent menu clicks update both the atomic.Bools
	// and persist to disk.
	settings.HoldToTalk.Store(cfg.HoldToTalk)
	settings.SoundsEnabled.Store(cfg.SoundsEnabled)
	settings.AutoPaste.Store(cfg.AutoPaste)
	settings.AIPostProcess.Store(flagClient.AIPostProcess())
	settings.PromptMode.Store(flagClient.PromptMode())
	settings.VoiceCommands.Store(flagClient.VoiceCommands())
	settings.ContextAware.Store(flagClient.ContextAware())

	// Initialize the menubar UI on the main goroutine. Must happen before the
	// hotkey listener registers on the main run loop, because uiInit creates
	// [NSApplication sharedApplication] and CGEventTap relies on its run loop.
	ui.Init(hotkeyLabel)
	ui.SetState(ui.StateIdle)
	ui.SetHotkeyPresets(hotkeyPresets, cfg.Hotkey)
	ui.SetMode(cfg.HoldToTalk)
	ui.SetSoundsEnabled(cfg.SoundsEnabled)
	ui.SetAutoPaste(cfg.AutoPaste)
	ui.SetAIPostProcess(settings.AIPostProcess.Load())
	ui.SetPromptMode(settings.PromptMode.Load())
	ui.SetVoiceCommands(settings.VoiceCommands.Load())
	ui.SetContextAware(settings.ContextAware.Load())

	// Create hotkey listener and register the CGEventTap source on the main
	// run loop. Non-blocking: events arrive once we call ui.Run() below.
	listener := hotkey.NewListener(cfg.Triggers)
	if err := listener.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Print active features.
	fmt.Printf("Hotkey: %s (%s mode)\n", hotkeyLabel, modeLabel(cfg.HoldToTalk))
	if claudeClient != nil {
		var features []string
		if settings.AIPostProcess.Load() {
			features = append(features, "AI post-processing")
		}
		if settings.PromptMode.Load() {
			features = append(features, "prompt mode")
		}
		if settings.VoiceCommands.Load() {
			features = append(features, "voice commands")
		}
		if settings.ContextAware.Load() {
			features = append(features, "context-aware")
		}
		if len(features) > 0 {
			fmt.Printf("AI:     %s (model: %s)\n", strings.Join(features, ", "), flagClient.AIModel())
		}
	}
	fmt.Println("   Quit from the menubar icon or send SIGINT/SIGTERM.")
	fmt.Println()
	fmt.Printf("Ready — %s to dictate.\n\n", hotkeyLabel)

	// Build the processing pipeline with all features.
	pipe := pipeline.New(
		transcribeStage(whisperClient, transcribeOpts),
		filterBlankStage(),
		classifyStage(),
		postProcessStage(claudeClient),
		promptModeStage(promptExec),
		commandStage(cmdRegistry),
		injectStage(),
	)

	// Signal handling — combined with the menubar Quit menu item via a
	// single shutdown watcher.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go shutdownWatcher(cancel, logger, recorder, sigCh)
	go showLogWatcher(ctx, logger)
	go hotkeyChangeWatcher(ctx, logger, listener)
	go settingsWatcher(ctx, logger, recorder)
	go runEventLoop(ctx, cfg, logger, listener, recorder, pipe)

	// Run NSApp's main loop on the main goroutine. Returns when the user
	// chooses Quit Vox in the menubar or shutdownWatcher calls ui.Quit().
	ui.Run()
}

// shutdownWatcher waits for either an OS signal or a menubar Quit click,
// then cancels the context (so watcher goroutines exit), drains the
// recorder, and tells NSApp to terminate.
func shutdownWatcher(cancel context.CancelFunc, logger *slog.Logger, recorder *audio.Recorder, sigCh <-chan os.Signal) {
	select {
	case <-sigCh:
	case <-ui.OnQuit():
	}
	cancel() // signal all watcher goroutines to stop
	cleanup(logger, recorder)
	if p := os.Getenv("VOX_LOG_PATH"); p != "" {
		_ = os.Remove(p)
	}
	if p := os.Getenv("VOX_PID_PATH"); p != "" {
		_ = os.Remove(p)
	}
	ui.Quit()
}

// hotkeyPresets is the curated list shown in the "Change Hotkey" submenu.
// Order matters — it's the order shown to the user. The Spec strings are
// what gets persisted to preferences.json and parsed by config.ParseHotkeys,
// so they must round-trip through that parser cleanly.
var hotkeyPresets = []ui.HotkeyPreset{
	{Spec: "option+space", Label: "Option + Space"},
	{Spec: "fn", Label: "Fn / Globe"},
	{Spec: "cmd+shift", Label: "Cmd + Shift (hold both)"},
	{Spec: "ctrl+option", Label: "Ctrl + Option (hold both)"},
	{Spec: "option+v", Label: "Option + V"},
	{Spec: "ctrl+shift+space", Label: "Ctrl + Shift + Space"},
}

func triggerLabel(triggers []hotkey.Trigger) string {
	var labels []string
	for _, t := range triggers {
		labels = append(labels, t.Label)
	}
	return strings.Join(labels, " or ")
}

// hotkeyChangeWatcher listens for hotkey picks from the menubar and applies
// them live to the running listener. The new value is also persisted to
// preferences.json so it survives restarts.
//
// VOX_HOTKEY env var is intentionally NOT updated — the env var is a
// per-process override; the menu is the persistent source of truth.
func hotkeyChangeWatcher(ctx context.Context, logger *slog.Logger, listener *hotkey.Listener) {
	for {
		select {
		case <-ctx.Done():
			return
		case spec := <-ui.OnHotkeyChange():
			triggers, err := config.ParseHotkeys(spec)
			if err != nil {
				logger.Warn("invalid hotkey spec from menu", "spec", spec, "error", err)
				continue
			}
			listener.SetTriggers(triggers)
			label := triggerLabel(triggers)
			ui.SetHotkeyLabel(label)
			ui.SetHotkeyCheckmark(spec)
			if err := config.SavePref(func(p *config.Prefs) { p.Hotkey = spec }); err != nil {
				logger.Warn("save prefs", "error", err)
			}
			fmt.Printf("Hotkey changed to %s\n", label)
		}
	}
}

// settingsWatcher fans the menubar toggle events out to the runtime settings
// store and persists each change to preferences.json. Pause is special: if
// the user pauses mid-recording, we discard the in-flight audio so the menu
// click feels instantaneous. Pause itself is *not* persisted: a fresh launch
// should always be active so users don't get stuck in a silently-paused vox
// after a reboot.
func settingsWatcher(ctx context.Context, logger *slog.Logger, recorder *audio.Recorder) {
	save := func(name string, mutate func(*config.Prefs)) {
		if err := config.SavePref(mutate); err != nil {
			logger.Warn("save prefs", "field", name, "error", err)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case paused := <-ui.OnPauseToggle():
			settings.Paused.Store(paused)
			ui.SetPaused(paused)
			if paused && recorder.IsRecording() {
				// Only abort the in-flight recording if no stop-and-process
				// cycle has already claimed it. If processMu is held, the
				// keyup that started the cycle happened before this pause,
				// so let that transcription finish.
				if processMu.TryLock() {
					if _, err := recorder.Stop(); err != nil && !errors.Is(err, audio.ErrNotRecording) {
						logger.Warn("stop on pause", "error", err)
					}
					ui.SetState(ui.StateIdle)
					processMu.Unlock()
				}
			}
			fmt.Printf("Vox %s\n", boolWord(paused, "paused", "resumed"))
		case hold := <-ui.OnModeChange():
			settings.HoldToTalk.Store(hold)
			ui.SetMode(hold)
			save("hold_to_talk", func(p *config.Prefs) { p.HoldToTalk = config.BoolPtr(hold) })
			fmt.Printf("Mode: %s\n", boolWord(hold, "hold to talk", "toggle"))
		case on := <-ui.OnSoundsToggle():
			settings.SoundsEnabled.Store(on)
			ui.SetSoundsEnabled(on)
			save("sounds_enabled", func(p *config.Prefs) { p.SoundsEnabled = config.BoolPtr(on) })
		case on := <-ui.OnAutoPasteToggle():
			settings.AutoPaste.Store(on)
			ui.SetAutoPaste(on)
			save("auto_paste", func(p *config.Prefs) { p.AutoPaste = config.BoolPtr(on) })
		case on := <-ui.OnAIPostProcessToggle():
			settings.AIPostProcess.Store(on)
			ui.SetAIPostProcess(on)
			save("ai_postprocess", func(p *config.Prefs) { p.AIPostProcess = config.BoolPtr(on) })
		case on := <-ui.OnPromptModeToggle():
			settings.PromptMode.Store(on)
			ui.SetPromptMode(on)
			save("prompt_mode", func(p *config.Prefs) { p.PromptMode = config.BoolPtr(on) })
		case on := <-ui.OnVoiceCommandsToggle():
			settings.VoiceCommands.Store(on)
			ui.SetVoiceCommands(on)
			save("voice_commands", func(p *config.Prefs) { p.VoiceCommands = config.BoolPtr(on) })
		case on := <-ui.OnContextAwareToggle():
			settings.ContextAware.Store(on)
			ui.SetContextAware(on)
			save("context_aware", func(p *config.Prefs) { p.ContextAware = config.BoolPtr(on) })
		}
	}
}

func boolWord(b bool, ifTrue, ifFalse string) string {
	if b {
		return ifTrue
	}
	return ifFalse
}

// showLogWatcher opens VOX_LOG_PATH (or a sensible default) in Console.app
// whenever the user picks "Show Log…" from the menubar.
func showLogWatcher(ctx context.Context, logger *slog.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ui.OnShowLog():
			path := os.Getenv("VOX_LOG_PATH")
			if path == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					logger.Warn("show log: cannot resolve home directory", "error", err)
					continue
				}
				path = filepath.Join(home, "Library", "Logs", "vox.log")
			}
			if _, err := os.Stat(path); err != nil {
				logger.Warn("show log: file not found", "path", path)
				continue
			}
			// Open in Console.app for syntax highlighting and tail-following.
			// Use go cmd.Wait() to reap the child process and avoid zombies.
			cmd := exec.Command("open", "-a", "Console", path)
			if err := cmd.Start(); err != nil {
				// Fall back to whatever app is configured for .log files.
				cmd = exec.Command("open", path)
				if err := cmd.Start(); err == nil {
					go cmd.Wait()
				}
			} else {
				go cmd.Wait()
			}
		}
	}
}

func runSetup() {
	fmt.Print(banner)
	fmt.Println("Setup")
	fmt.Println("=====")
	fmt.Println()

	// Step 1: Permissions.
	fmt.Println("[1/3] Permissions")

	fmt.Print("  Accessibility: ")
	if hotkey.CheckAccessibility() {
		fmt.Println("granted")
	} else {
		fmt.Println("requested — grant in System Settings > Privacy & Security > Accessibility")
	}

	fmt.Print("  Microphone:    ")
	if hotkey.RequestMicrophoneAccess() {
		fmt.Println("granted")
	} else {
		fmt.Println("denied — grant in System Settings > Privacy & Security > Microphone")
	}
	fmt.Println()

	// Step 2: Dependencies.
	fmt.Println("[2/3] Dependencies")
	checkDep("sox (rec)", "rec")
	checkDep("ffmpeg", "ffmpeg")
	checkDep("whisper-server", "whisper-server")
	fmt.Println()

	// Step 3: AI features.
	fmt.Println("[3/3] AI Features")
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		fmt.Println("  ANTHROPIC_API_KEY: set")
		fmt.Println("  Run 'vox config' to enable AI features (post-processing, prompt mode, etc.)")
	} else {
		fmt.Println("  ANTHROPIC_API_KEY: not set (AI features disabled)")
		fmt.Println("  Set ANTHROPIC_API_KEY to enable AI post-processing, prompt mode, and voice commands.")
	}
	if os.Getenv("VOX_LD_SDK_KEY") != "" {
		fmt.Println("  VOX_LD_SDK_KEY:   set (LaunchDarkly flag control enabled)")
	}
	fmt.Println()

	fmt.Println("Done. Next steps:")
	fmt.Println("  vox config    Configure AI features interactively")
	fmt.Println("  make start    Start whisper + vox")
	fmt.Println("  vox help      See all options")
}

func checkDep(name, binary string) {
	if _, err := exec.LookPath(binary); err == nil {
		fmt.Printf("  %s: installed\n", name)
	} else {
		fmt.Printf("  %s: not found\n", name)
	}
}

// runEventLoop is the single hotkey handler. The hold-vs-toggle behavior is
// chosen per event based on settings.HoldToTalk so the user can swap modes
// from the menu without restarting the loop.
//
// All hotkey events are dropped when settings.Paused is true.
func runEventLoop(
	ctx context.Context,
	cfg config.Config,
	logger *slog.Logger,
	listener *hotkey.Listener,
	recorder *audio.Recorder,
	pipe *pipeline.Pipeline,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-listener.Keydown():
			if settings.Paused.Load() {
				continue
			}
			if settings.HoldToTalk.Load() {
				startRecording(logger, recorder, cfg.Verbose, "Recording...")
			} else {
				if recorder.IsRecording() {
					playStop()
					handleStopAndProcess(ctx, cfg, logger, recorder, pipe)
				} else {
					startRecording(logger, recorder, cfg.Verbose, "Recording... (press again to stop)")
				}
			}
		case <-listener.Keyup():
			if settings.Paused.Load() {
				continue
			}
			if !settings.HoldToTalk.Load() {
				continue
			}
			if !recorder.IsRecording() {
				continue
			}
			playStop()
			handleStopAndProcess(ctx, cfg, logger, recorder, pipe)
		}
	}
}

func startRecording(logger *slog.Logger, recorder *audio.Recorder, verbose bool, banner string) {
	if verbose {
		logger.Debug("hotkey pressed, starting recording")
	}
	ui.SetState(ui.StateRecording)
	playStart()
	fmt.Println(banner)
	if err := recorder.Start(); err != nil {
		fmt.Printf("Error starting recording: %v\n", err)
		ui.SetState(ui.StateIdle)
	}
}

func playStart() {
	if settings.SoundsEnabled.Load() {
		audio.PlayStartSound()
	}
}

func playStop() {
	if settings.SoundsEnabled.Load() {
		audio.PlayStopSound()
	}
}

func handleStopAndProcess(
	ctx context.Context,
	cfg config.Config,
	logger *slog.Logger,
	recorder *audio.Recorder,
	pipe *pipeline.Pipeline,
) {
	// Claim the recorder for the entire stop-and-process cycle so a pause
	// click can't steal the WAV out from under us between SetState and Stop.
	processMu.Lock()
	defer processMu.Unlock()

	ui.SetState(ui.StateTranscribing)
	defer ui.SetState(ui.StateIdle)

	fmt.Println("Transcribing...")

	wavData, err := recorder.Stop()
	if err != nil {
		if errors.Is(err, audio.ErrTooShort) {
			fmt.Println("Too short, skipping.")
			fmt.Println("Ready!")
			return
		}
		if errors.Is(err, audio.ErrNotRecording) {
			// Belt-and-suspenders: the mutex should make this unreachable,
			// but if shutdown or some future caller stops the recorder
			// concurrently, we treat it as "nothing to transcribe" rather
			// than logging a misleading error.
			fmt.Println("Ready!")
			return
		}
		fmt.Printf("Error stopping recording: %v\n", err)
		fmt.Println("Ready!")
		return
	}

	if cfg.Verbose {
		logger.Debug("recorded audio", "bytes", len(wavData))
	}

	r := &pipeline.Result{RawAudio: wavData}
	if err := pipe.Run(ctx, r); err != nil {
		fmt.Printf("Error processing: %v\n", err)
		fmt.Println("Ready!")
		return
	}

	if r.Cancelled {
		fmt.Println("(no speech detected)")
	}

	fmt.Println("Ready!")
}

// transcribeStage returns a pipeline stage that sends audio to the Whisper API.
func transcribeStage(client *transcribe.Client, opts transcribe.TranscribeOptions) pipeline.Stage {
	return func(ctx context.Context, r *pipeline.Result) error {
		text, err := client.Transcribe(ctx, r.RawAudio, opts)
		if err != nil {
			return fmt.Errorf("transcribing: %w", err)
		}
		r.RawText = text
		r.OutputText = text
		return nil
	}
}

// filterBlankStage returns a pipeline stage that cancels processing when the
// transcription is blank or a whisper hallucination artifact.
func filterBlankStage() pipeline.Stage {
	return func(_ context.Context, r *pipeline.Result) error {
		if r.RawText == "" || isBlankAudio(r.RawText) {
			r.Cancelled = true
		}
		return nil
	}
}

// injectStage returns a pipeline stage that either pastes the output text
// into the focused application (Auto-paste on, the default) or just copies
// it to the clipboard (Auto-paste off). Either way, the menubar updates
// its "Last:" row so the user can confirm what was captured.
func injectStage() pipeline.Stage {
	return func(_ context.Context, r *pipeline.Result) error {
		ui.SetLastText(r.OutputText)
		if settings.AutoPaste.Load() {
			if err := inject.TypeText(r.OutputText); err != nil && !errors.Is(err, inject.ErrNotSupported) {
				fmt.Printf("Error pasting text: %v\n", err)
			}
			return nil
		}
		if err := inject.CopyToClipboard(r.OutputText); err != nil && !errors.Is(err, inject.ErrNotSupported) {
			fmt.Printf("Error copying to clipboard: %v\n", err)
		}
		return nil
	}
}

// classifyStage determines if speech is dictation, a prompt, or a command.
func classifyStage() pipeline.Stage {
	return func(_ context.Context, r *pipeline.Result) error {
		// Only classify if prompt mode or voice commands are enabled.
		if !settings.PromptMode.Load() && !settings.VoiceCommands.Load() {
			return nil
		}
		intent := classify.Classify(r.RawText)
		switch intent.Mode {
		case classify.ModePrompt:
			if settings.PromptMode.Load() {
				r.Mode = pipeline.ModePrompt
				r.Metadata = map[string]string{
					"action":  intent.Action,
					"subject": intent.Subject,
					"args":    intent.RawArgs,
				}
			}
		case classify.ModeCommand:
			if settings.VoiceCommands.Load() {
				r.Mode = pipeline.ModeCommand
				r.Metadata = map[string]string{
					"action": intent.Action,
					"args":   intent.RawArgs,
				}
			}
		}
		return nil
	}
}

// postProcessStage sends dictation text through Claude for grammar/punctuation cleanup.
func postProcessStage(cc *claude.Client) pipeline.Stage {
	return func(ctx context.Context, r *pipeline.Result) error {
		if cc == nil || !settings.AIPostProcess.Load() || r.Mode != pipeline.ModeDictation {
			return nil
		}

		systemPrompt := claude.PostProcessSystemPrompt
		if settings.ContextAware.Load() {
			app := appctx.Detect()
			systemPrompt = format.SystemPromptWithHint(systemPrompt, app.Category())
		}

		processed, err := cc.Complete(ctx, systemPrompt, r.RawText)
		if err != nil {
			// Non-fatal: fall back to raw text.
			fmt.Printf("Warning: AI post-processing failed: %v\n", err)
			return nil
		}
		r.ProcessedText = processed
		r.OutputText = processed
		return nil
	}
}

// promptModeStage handles prompt-mode actions (summarize, explain, rewrite, etc.).
func promptModeStage(exec *prompt.Executor) pipeline.Stage {
	return func(ctx context.Context, r *pipeline.Result) error {
		if r.Mode != pipeline.ModePrompt || exec == nil || !settings.PromptMode.Load() {
			return nil
		}
		action := r.Metadata["action"]
		subject := r.Metadata["subject"]
		args := r.Metadata["args"]

		fmt.Printf("[prompt: %s] ", action)
		result, err := exec.Execute(ctx, action, subject, args)
		if err != nil {
			return fmt.Errorf("prompt mode (%s): %w", action, err)
		}
		r.OutputText = result
		return nil
	}
}

// commandStage executes voice commands (create PR, query flag, etc.).
func commandStage(reg *commands.Registry) pipeline.Stage {
	return func(ctx context.Context, r *pipeline.Result) error {
		if r.Mode != pipeline.ModeCommand || !settings.VoiceCommands.Load() {
			return nil
		}
		action := r.Metadata["action"]
		args := r.Metadata["args"]

		fmt.Printf("[command: %s] ", action)
		result, err := reg.Execute(ctx, action, args)
		if err != nil {
			return fmt.Errorf("voice command (%s): %w", action, err)
		}
		r.OutputText = result
		return nil
	}
}

func modeLabel(holdToTalk bool) string {
	if holdToTalk {
		return "hold-to-talk"
	}
	return "toggle"
}

// isBlankAudio returns true if the transcription is a whisper hallucination
// artifact rather than real speech (e.g. "[BLANK_AUDIO]", "[blank audio]", "(blank audio)").
func isBlankAudio(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	t = strings.Trim(t, "[]() ")
	return t == "blank audio" || t == "blank_audio"
}

func cleanup(logger *slog.Logger, recorder *audio.Recorder) {
	if recorder.IsRecording() {
		if _, err := recorder.Stop(); err != nil && !errors.Is(err, audio.ErrNotRecording) {
			logger.Warn("error stopping recorder during shutdown", "error", err)
		}
	}
	fmt.Println("\nStopped.")
}
