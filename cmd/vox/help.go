package main

import "fmt"

func runHelp() {
	fmt.Print(banner)
	fmt.Println("Usage: vox [command]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  (none)     Start dictation (default)")
	fmt.Println("  setup      Check Accessibility + Microphone permissions")
	fmt.Println("  test       Run self-diagnosis across all subsystems")
	fmt.Println("  help       Show this help message")
	fmt.Println("  version    Show version")
	fmt.Println()
	fmt.Println("Environment Variables:")
	fmt.Println("  VOX_HOTKEY          Hotkey (default: option+space)")
	fmt.Println("  VOX_HOLD_TO_TALK    Hold-to-talk mode (default: true)")
	fmt.Println("  VOX_LANGUAGE        Language code (default: auto-detect)")
	fmt.Println("  VOX_VERBOSE         Debug logging (default: false)")
	fmt.Println("  VOX_WHISPER_MODEL_ID Initial model ID (default: base.en)")
	fmt.Println()
	fmt.Println("Quick Start:")
	fmt.Println("  1. make setup           # install deps + check permissions")
	fmt.Println("  2. make start           # start vox (manages whisper locally)")
	fmt.Println("  3. Hold Option+Space    # speak, release to paste")
	fmt.Println()
	fmt.Println("Docs: https://github.com/mattthewong/vox")
}
