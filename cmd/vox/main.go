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
	"vox/internal/commandconfig"
	"vox/internal/commands"
	"vox/internal/config"
	"vox/internal/flags"
	"vox/internal/format"
	"vox/internal/hotkey"
	"vox/internal/inject"
	"vox/internal/notify"
	"vox/internal/pipeline"
	"vox/internal/prompt"
	"vox/internal/sttmodel"
	"vox/internal/transcribe"
	"vox/internal/ui"
	"vox/internal/userconfig"
	"vox/internal/vocab"
)

// notifier posts native OS notifications for state changes the user cannot
// see in the terminal: an engine falling back at startup, or a menubar model
// switch that failed. The notify package exposes a Notifier interface rather
// than a package-level Send, so construct one here and share it.
var notifier = notify.New()

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

// currentModelID holds the catalog model ID (e.g. base.en, parakeet-v2) loaded
// by the active engine so removing a model can switch away first.
var currentModelID atomic.Value // string

// modelMu serializes model-change and model-delete operations so they cannot
// interleave (e.g. switching to a model while a delete is mid-fallback).
var modelMu sync.Mutex

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
		case "test":
			runTest()
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

	// Ensure only one instance of vox is running.
	lockFile, err := acquireLock()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: vox is already running.")
		fmt.Fprintln(os.Stderr, "  Kill the other instance first, or run: make stop")
		os.Exit(1)
	}
	defer lockFile.Close()

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

	// Resolve the configured engine/model and bring up its backend.
	selectedModel, err := resolveEngineModel(cfg.ModelID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if !sttmodel.IsInstalled(selectedModel) {
		fmt.Printf("Selected model %q is not installed, downloading (%d MiB)...\n",
			selectedModel.ID, selectedModel.SizeMB)
	}
	activeEngine, degraded, startErr := startEngineWithFallback(ctx, selectedModel, whisperLogPath(),
		func(done, total int64) {
			if total > 0 {
				fmt.Printf("\r  %d%%", done*100/total)
			}
		})
	// A nil engine is the only fatal case. A non-nil engine with a non-nil
	// error means "running, but degraded" — do not exit.
	if activeEngine == nil {
		fmt.Fprintf(os.Stderr, "Error starting speech engine: %v\n", startErr)
		os.Exit(1)
	}
	if degraded {
		logger.Warn("stt engine fell back",
			"requested", selectedModel.ID,
			"active", activeEngine.model.ID,
			"error", startErr)
		fmt.Printf("Warning: could not start %s (%v)\n", selectedModel.ID, startErr)
		fmt.Printf("  Falling back to %s.\n\n", activeEngine.model.ID)
		notifier.Send("Vox",
			fmt.Sprintf("%s unavailable, using %s", selectedModel.ID, activeEngine.model.ID))
		selectedModel = activeEngine.model
	}
	if err := activeEngine.healthCheck(ctx); err != nil {
		fmt.Printf("Warning: %s backend unavailable (%v)\n", selectedModel.Engine, err)
		fmt.Println("  Transcription will fail until it is reachable.")
		fmt.Println()
	} else if cfg.Verbose {
		logger.Debug("stt engine ready", "engine", selectedModel.Engine, "model", selectedModel.ID)
	}

	// engines is the indirection the pipeline reads through, so a menubar
	// engine switch does not require rebuilding the pipeline.
	engines := &engineHolder{}
	engines.set(activeEngine)

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

	// Initialize Claude API client. Key can come from:
	//   1. ANTHROPIC_API_KEY env var (personal key)
	//   2. vox-anthropic-key LD flag (team-managed key)
	//   3. ~/.vox/config.yaml anthropic_key field
	var claudeClient *claude.Client
	if apiKey, keySource := flagClient.AnthropicKey(); apiKey != "" {
		claudeClient = claude.NewClient(apiKey, flagClient.AIModel())
		if cfg.Verbose {
			logger.Debug("Claude API client initialized", "model", flagClient.AIModel(), "key_source", keySource)
		}
	}

	// Initialize prompt mode executor.
	var promptExec *prompt.Executor
	if claudeClient != nil {
		promptExec = prompt.NewExecutor(claudeClient, inject.ReadClipboard)
	}

	// Load user-defined voice commands (warn on error, don't fatal).
	userDefs, loadErr := commandconfig.Load(commandconfig.DefaultPath())
	if loadErr != nil {
		slog.Warn("loading custom commands", "error", loadErr)
	}

	// Merge user commands with built-in defaults.
	mergedCmds, mergedPrefixes := commandconfig.Merge(
		commands.DefaultCommands(),
		userDefs,
	)

	cmdRegistry := commands.NewRegistry(mergedCmds...)
	classifier := classify.NewClassifier(mergedPrefixes)

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
	currentModelID.Store(selectedModel.ID)
	refreshModelMenus()
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
		transcribeStage(engines, transcribeOpts),
		filterBlankStage(),
		classifyStage(classifier),
		postProcessStage(claudeClient),
		promptModeStage(promptExec),
		commandStage(cmdRegistry),
		injectStage(),
	)

	// Signal handling — combined with the menubar Quit menu item via a
	// single shutdown watcher.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go shutdownWatcher(cancel, logger, recorder, engines, sigCh)
	go showLogWatcher(ctx, logger)
	go hotkeyChangeWatcher(ctx, logger, listener)
	go settingsWatcher(ctx, logger, recorder)
	go modelChangeWatcher(ctx, logger, engines, recorder)
	go modelDeleteWatcher(ctx, logger, engines, recorder)
	go runEventLoop(ctx, cfg, logger, listener, recorder, pipe)

	// Run NSApp's main loop on the main goroutine. Returns when the user
	// chooses Quit Vox in the menubar or shutdownWatcher calls ui.Quit().
	ui.Run()
}

// shutdownWatcher waits for either an OS signal or a menubar Quit click,
// then cancels the context (so watcher goroutines exit), drains the
// recorder, and tells NSApp to terminate.
func shutdownWatcher(cancel context.CancelFunc, logger *slog.Logger, recorder *audio.Recorder, engines *engineHolder, sigCh <-chan os.Signal) {
	select {
	case <-sigCh:
	case <-ui.OnQuit():
	}
	cancel() // signal all watcher goroutines to stop
	cleanup(logger, recorder)
	if err := engines.stopActive(context.Background()); err != nil {
		logger.Warn("stop stt engine", "error", err)
	}
	if p := os.Getenv("VOX_LOG_PATH"); p != "" {
		_ = os.Remove(p)
	}
	if p := os.Getenv("VOX_PID_PATH"); p != "" {
		_ = os.Remove(p)
	}
	ui.Quit()
}

func buildModelPresets() []ui.ModelPreset {
	models := sttmodel.All()
	presets := make([]ui.ModelPreset, 0, len(models))
	for _, m := range models {
		presets = append(presets, ui.ModelPreset{
			ID:         m.ID,
			Label:      m.Label,
			Engine:     string(m.Engine),
			Descriptor: m.Descriptor,
			Badge:      m.Badge,
			Blurb:      m.Blurb,
			Installed:  sttmodel.IsInstalled(m),
		})
	}
	return presets
}

func buildModelRemovePresets() []ui.ModelRemovePreset {
	var out []ui.ModelRemovePreset
	nInstalled := sttmodel.InstalledCount()
	for _, m := range sttmodel.All() {
		if sttmodel.IsInstalled(m) {
			label := m.Label
			if nInstalled <= 1 {
				label = m.Label + " (required)"
			}
			out = append(out, ui.ModelRemovePreset{
				ID:        m.ID,
				Label:     label,
				Removable: nInstalled > 1,
			})
		}
	}
	return out
}

func refreshModelMenus() {
	id, _ := currentModelID.Load().(string)
	ui.SetModelPresets(buildModelPresets(), id)
	ui.SetModelRemovePresets(buildModelRemovePresets())
}

// activateModel switches to the given model, downloading it if needed and
// tearing down the previous engine's backend.
//
// Precondition: caller must hold modelMu. activateModel reads engines.get()
// outside the write lock and mutates the returned state inside it; concurrent
// callers would race on the engine pointer without external serialization.
func activateModel(ctx context.Context, engines *engineHolder, recorder *audio.Recorder, model sttmodel.Model) error {
	if !sttmodel.IsInstalled(model) {
		err := sttmodel.Download(ctx, model, func(downloaded, total int64) {
			if total > 0 {
				pct := (downloaded * 100) / total
				ui.SetStatusLine(fmt.Sprintf("Status: Downloading %s (%d%%)…", model.ID, pct))
				return
			}
			ui.SetStatusLine(fmt.Sprintf("Status: Downloading %s…", model.ID))
		})
		if err != nil {
			return err
		}
	}

	ui.SetStatusLine(fmt.Sprintf("Status: Switching to %s…", model.ID))
	processMu.Lock()
	defer processMu.Unlock()

	// Recording holds the audio path; swapping engines underneath it would
	// drop the in-flight utterance. Note this guard only covers the recording
	// window — the transcription window is covered by engineHolder's write
	// lock, because by then the recorder has already stopped.
	if recorder != nil && recorder.IsRecording() {
		return fmt.Errorf("cannot switch models while recording")
	}

	prev := engines.get()

	// Fast path: same engine family and a running whisper server, so just
	// swap the loaded model instead of restarting the process.
	//
	// This mutates the live engine in place rather than going through
	// engineHolder.swap, so it must not run concurrently with a
	// transcription. whisperserver.Switch stops and restarts the child
	// process underneath the HTTP client. Take the holder's write lock for
	// the duration.
	if prev != nil && prev.whisperSrv != nil && model.Engine == sttmodel.EngineWhisper {
		// ResolvePath, not Path: honor a legacy install location so switching
		// to a pre-split model does not point whisper-server at a path that
		// has nothing in it.
		path, err := sttmodel.ResolvePath(model)
		if err != nil {
			return err
		}
		return engines.withWriteLock(func() error {
			if err := prev.whisperSrv.Switch(ctx, path); err != nil {
				return err
			}
			prev.whisperClient.ResetEndpoint()
			prev.model = model
			return nil
		})
	}

	next, err := startEngine(ctx, model, whisperLogPath(), nil)
	if err != nil {
		return err
	}
	// swap installs the new engine and stops the old one under the write
	// lock, so no transcription can be running against the old backend.
	if err := engines.swap(ctx, next); err != nil {
		return fmt.Errorf("switched to %s but failed to stop previous engine: %w", model.ID, err)
	}
	return nil
}

// pickFallbackModel chooses another installed model to switch to when the
// active one is being deleted. Same-engine models are preferred so the user
// does not silently jump between whisper and parakeet.
func pickFallbackModel(excludeID string, preferred sttmodel.Engine) (sttmodel.Model, bool) {
	for _, m := range sttmodel.ByEngine(preferred) {
		if m.ID != excludeID && sttmodel.IsInstalled(m) {
			return m, true
		}
	}
	for _, m := range sttmodel.All() {
		if m.ID != excludeID && sttmodel.IsInstalled(m) {
			return m, true
		}
	}
	return sttmodel.Model{}, false
}

func whisperLogPath() string {
	if p := os.Getenv("VOX_LOG_PATH"); p != "" {
		return filepath.Join(filepath.Dir(p), "whisper.log")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "whisper.log"
	}
	return filepath.Join(home, "Library", "Logs", "whisper.log")
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

func modelChangeWatcher(ctx context.Context, logger *slog.Logger, engines *engineHolder, recorder *audio.Recorder) {
	for {
		select {
		case <-ctx.Done():
			return
		case modelID := <-ui.OnModelChange():
			modelMu.Lock()
			model, ok := sttmodel.ByID(modelID)
			if !ok {
				logger.Warn("unknown model selected", "id", modelID)
				modelMu.Unlock()
				continue
			}

			ui.SetModelMenuEnabled(false)
			ui.SetStatusLine(fmt.Sprintf("Status: Preparing model %s…", model.ID))

			if err := activateModel(ctx, engines, recorder, model); err != nil {
				logger.Error("switch model", "model", model.ID, "error", err)
				ui.SetStatusLine(fmt.Sprintf("Status: Failed to load %s", model.ID))
				ui.SetModelMenuEnabled(true)
				// refreshModelMenus restores the checkmark to the model that
				// is still active, so the menu does not claim a switch that
				// did not happen.
				refreshModelMenus()
				notifier.Send("Vox", fmt.Sprintf("Could not switch to %s: %v", model.ID, err))
				modelMu.Unlock()
				continue
			}

			currentModelID.Store(model.ID)
			refreshModelMenus()
			ui.SetStatusLine("Status: Idle")
			ui.SetModelMenuEnabled(true)
			if err := config.SavePref(func(p *config.Prefs) { p.Model = model.ID }); err != nil {
				logger.Warn("save prefs", "field", "model", "error", err)
			}
			fmt.Printf("Model changed to %s\n", model.Label)
			modelMu.Unlock()
		}
	}
}

func modelDeleteWatcher(ctx context.Context, logger *slog.Logger, engines *engineHolder, recorder *audio.Recorder) {
	for {
		select {
		case <-ctx.Done():
			return
		case modelID := <-ui.OnModelDelete():
			modelMu.Lock()
			model, ok := sttmodel.ByID(modelID)
			if !ok || !sttmodel.IsInstalled(model) {
				modelMu.Unlock()
				continue
			}
			if sttmodel.InstalledCount() <= 1 {
				logger.Info("refusing remove: last downloaded model", "id", modelID)
				ui.SetStatusLine("Status: Idle")
				fmt.Printf("Can't remove your only downloaded model.\n")
				modelMu.Unlock()
				continue
			}

			ui.SetModelMenuEnabled(false)
			ui.SetStatusLine(fmt.Sprintf("Status: Removing %s…", model.ID))

			active, _ := currentModelID.Load().(string)
			if modelID == active {
				// Prefer a fallback from the same engine family so deleting a
				// model does not silently move the user between whisper and
				// parakeet.
				fallback, ok := pickFallbackModel(modelID, model.Engine)
				if !ok {
					logger.Warn("delete active model: no installed fallback", "id", modelID)
					ui.SetStatusLine("Status: Idle")
					ui.SetModelMenuEnabled(true)
					modelMu.Unlock()
					continue
				}
				if err := activateModel(ctx, engines, recorder, fallback); err != nil {
					logger.Warn("switch before model delete", "id", modelID, "fallback", fallback.ID, "error", err)
					ui.SetStatusLine("Status: Idle")
					ui.SetModelMenuEnabled(true)
					refreshModelMenus()
					modelMu.Unlock()
					continue
				}
				currentModelID.Store(fallback.ID)
				if err := config.SavePref(func(p *config.Prefs) { p.Model = fallback.ID }); err != nil {
					logger.Warn("save prefs", "field", "model", "error", err)
				}
			}

			if err := sttmodel.Remove(model); err != nil {
				logger.Warn("remove model file", "id", model.ID, "error", err)
				ui.SetStatusLine("Status: Remove failed")
			} else {
				fmt.Printf("Removed downloaded model: %s\n", model.Label)
			}

			refreshModelMenus()
			ui.SetStatusLine("Status: Idle")
			ui.SetModelMenuEnabled(true)
			modelMu.Unlock()
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
		fmt.Println("  Anthropic API key: set (via ANTHROPIC_API_KEY env var)")
	} else {
		fmt.Println("  Anthropic API key: not set via env var")
		fmt.Println("  AI features can be enabled by any of:")
		fmt.Println("    - ANTHROPIC_API_KEY env var (personal key)")
		fmt.Println("    - vox-anthropic-key LD flag (team-managed key)")
		fmt.Println("    - ~/.vox/config.yaml anthropic_key field")
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

// transcribeStage returns a pipeline stage that converts recorded audio to text.
func transcribeStage(client transcribe.Transcriber, opts transcribe.TranscribeOptions) pipeline.Stage {
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
func classifyStage(cl *classify.Classifier) pipeline.Stage {
	return func(_ context.Context, r *pipeline.Result) error {
		// Only classify if prompt mode or voice commands are enabled.
		if !settings.PromptMode.Load() && !settings.VoiceCommands.Load() {
			return nil
		}
		intent := cl.Classify(r.RawText)
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
			r.OutputText = fmt.Sprintf("%s: failed", action)
			return fmt.Errorf("voice command (%s): %w", action, err)
		}
		if result == "" {
			r.OutputText = fmt.Sprintf("%s: OK", action)
		} else {
			r.OutputText = fmt.Sprintf("%s: %s", action, result)
		}
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

// acquireLock attempts to acquire an exclusive lock on ~/.vox/vox.lock.
// Returns the open file (caller must defer Close) or an error if another
// instance holds the lock. The lock is released automatically when the
// process exits, even on crash or SIGKILL.
func acquireLock() (*os.File, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	lockPath := filepath.Join(home, ".vox", "vox.lock")

	// Ensure ~/.vox/ exists.
	if err := os.MkdirAll(filepath.Dir(lockPath), 0700); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}

	// Try non-blocking exclusive lock.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}

	return f, nil
}

func cleanup(logger *slog.Logger, recorder *audio.Recorder) {
	if recorder.IsRecording() {
		if _, err := recorder.Stop(); err != nil && !errors.Is(err, audio.ErrNotRecording) {
			logger.Warn("error stopping recorder during shutdown", "error", err)
		}
	}
	fmt.Println("\nStopped.")
}
