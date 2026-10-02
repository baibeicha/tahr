package main

import (
	"flag"
	"fmt"
	"os"

	"tahr/internal/core/plugin"
)

const (
	Version   = "0.1.0"
	BuildDate = "2026-09-30"
	Engine    = "GoatUI TEA Diff-Engine (v0.1.0)"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "plugin":
			if err := plugin.RunCLI(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "Plugin error: %v\n", err)
				os.Exit(1)
			}
			return

		case "--version", "-v", "version":
			printVersion()
			return

		case "--help", "-h", "help":
			printHelp()
			return
		}
	}

	var (
		themeFlag   = flag.String("theme", "", "Color theme (default loaded from settings: catppuccin, gruvbox, tokyo-night, dracula, nord, monokai)")
		guiFlag     = flag.Bool("gui", false, "Launch graphical user interface (Gio Desktop)")
		projectFlag = flag.String("project", "", "Path to project root directory")
		pFlag       = flag.String("p", "", "Path to project root directory (shorthand)")
		verFlag     = flag.Bool("v", false, "Print version and exit")
	)

	flag.Usage = printHelp
	flag.Parse()

	if *verFlag {
		printVersion()
		return
	}

	var projectDir string
	if *projectFlag != "" {
		projectDir = *projectFlag
	} else if *pFlag != "" {
		projectDir = *pFlag
	}

	rawArgs := flag.Args()
	var filesToOpen []string
	for _, arg := range rawArgs {
		if fi, err := os.Stat(arg); err == nil && fi.IsDir() {
			if projectDir == "" {
				projectDir = arg
			}
		} else {
			filesToOpen = append(filesToOpen, arg)
		}
	}

	if *guiFlag {
		if err := runGUI(projectDir, filesToOpen, *themeFlag); err != nil {
			fmt.Fprintf(os.Stderr, "GUI launch failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := runTUI(projectDir, filesToOpen, *themeFlag); err != nil {
		fmt.Fprintf(os.Stderr, "Tahr exited with error: %v\n", err)
		os.Exit(1)
	}
}

func printVersion() {
	fmt.Printf("Tahr Terminal & Graphical IDE v%s (%s)\n", Version, BuildDate)
	fmt.Printf("Rendering: %s\n", Engine)
	fmt.Println("High-Performance Augmented Rope Buffer, Multi-Cursor & Zstandard Plugin Architecture.")
}

func printHelp() {
	fmt.Println(`Tahr - High-performance Terminal & Graphical IDE

USAGE:
  tahr [OPTIONS] [FILES...]
  tahr plugin <COMMAND> [ARGS...]

OPTIONS:
  -project <path>  Open specified directory as project root
  -p <path>        Shorthand for -project
  -theme <name>    Set color theme: catppuccin (default), gruvbox, tokyo-night
  -gui             Launch Gio graphical frontend (requires -tags gui)
  -v, --version    Display version information
  -h, --help       Display this help message

PLUGIN COMMANDS:
  tahr plugin init <name> [--lang=go]   Scaffold new plugin project
  tahr plugin pack [dir] [dest.tahr]    Pack directory into Zstandard .tahr archive
  tahr plugin install <package.tahr>    Install plugin into ~/.config/tahr/plugins
  tahr plugin link <dir>                Symlink local plugin directory for development
  tahr plugin list                      List all installed plugins

KEYBINDINGS (Standard CUA & IDE Hotkeys - English & Russian layouts):
  F5 / Ctrl+R      Run project or active file
  F7 / Ctrl+Shift+B Build project
  F2 / Ctrl+B      Toggle Project Tree file explorer sidebar
  Ctrl+Alt+E       Toggle Project Tree dock (Left <-> Right)
  Ctrl+\           Cycle Split Layout (1 to 6 editor panes)
  Alt+1..6         Focus Split Pane 1..6
  Alt+Right/Left   Next / Previous Split Pane
  F4 / Ctrl+~      Toggle Integrated Terminal drawer
  F8               Toggle DAP Debugger HUD (Call Stack, Variables, Watch)
  F9               Toggle breakpoint on current line
  F10              DAP: Step Over line
  F11 / Shift+F11  DAP: Step Into / Step Out
  Shift+F5         DAP: Stop debugging session
  Ctrl+,           Settings & Plugin Marketplace modal
  Ctrl+O           Open or create project panel
  Ctrl+S           Save current document atomically
  Ctrl+P           Omnibar: fuzzy file search
  Ctrl+Shift+P     Command palette (Run, Build, Format, Breakpoints, Themes)
  Ctrl+C / Ctrl+V  Copy / Paste (with Bracketed Paste auto-indent defense)
  Ctrl+Z / Ctrl+Y  Undo / Redo (with intelligent word-level batching)
  Ctrl+F           Find in document
  Ctrl+G           Go to line in O(log N)
  F12              Go to definition
  Shift+K          Hover documentation
  Alt+Enter        Quick Fix & Code Actions popup
  Ctrl+Shift+G     Interactive Git Hunk Staging & Branch Graph
  Ctrl+Shift+M     Live Markdown ANSI/ASCII Preview Split
  Ctrl+Shift+T     Add new split terminal shell
  Ctrl+Shift+5     Toggle horizontal / vertical terminal splits
  Alt+M            Toggle Minimap
  Ctrl+D           Multi-cursor: select next occurrence of word
  Ctrl+A           Select all buffer contents
  Ctrl+Space       Autocomplete suggestions popup
  Alt+Click        Add cursor at mouse click position
  Tab / Shift+Tab  Indent / Dedent block
  Ctrl+Q           Quit editor`)
}
