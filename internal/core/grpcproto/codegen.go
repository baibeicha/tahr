package grpcproto

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Supported codegen target languages.
const (
	LanguageGo         = "Go"
	LanguagePython     = "Python"
	LanguageRust       = "Rust"
	LanguageTypeScript = "TypeScript"
	LanguageCpp        = "C++"
)

// SupportedLanguages lists all target languages available for code generation.
var SupportedLanguages = []string{
	LanguageGo,
	LanguagePython,
	LanguageRust,
	LanguageTypeScript,
	LanguageCpp,
}

// CodegenConfig contains parameters for invoking code generation.
type CodegenConfig struct {
	Language       string   // Target language: Go, Python, Rust, TypeScript, C++
	ProtoFiles     []string // Input .proto file paths
	ImportPaths    []string // -I or --proto_path directories
	OutputDir      string   // Destination folder for generated stubs
	GenerateGRPC   bool     // Generate gRPC client & server stubs
	CustomCompiler string   // Optional custom executable (defaults to "protoc")
	ExtraFlags     []string // Additional arbitrary CLI flags
}

// CodegenCommand represents the prepared executable command line.
type CodegenCommand struct {
	Executable string
	Args       []string
	Language   string
}

// CommandString formats the command and arguments as a single shell-escaped string.
func (c *CodegenCommand) CommandString() string {
	parts := append([]string{c.Executable}, c.Args...)
	return strings.Join(parts, " ")
}

// CodegenResult stores the execution summary of a codegen run.
type CodegenResult struct {
	Success   bool          `json:"success"`
	ExitCode  int           `json:"exit_code"`
	Command   string        `json:"command"`
	Stdout    string        `json:"stdout"`
	Stderr    string        `json:"stderr"`
	Duration  time.Duration `json:"duration"`
	Compiler  string        `json:"compiler"`
	HelpGuide string        `json:"help_guide,omitempty"`
}

// BuildCodegenCommand compiles arguments for protoc according to target language and gRPC settings.
func BuildCodegenCommand(cfg CodegenConfig) (*CodegenCommand, error) {
	if len(cfg.ProtoFiles) == 0 {
		return nil, fmt.Errorf("no proto files specified for codegen")
	}

	lang := cfg.Language
	if lang == "" {
		lang = LanguageGo
	}

	compiler := cfg.CustomCompiler
	if compiler == "" {
		compiler = "protoc"
	}

	outDir := cfg.OutputDir
	if outDir == "" {
		outDir = "."
	}

	var args []string

	// Import paths
	for _, ip := range cfg.ImportPaths {
		if ip != "" {
			args = append(args, fmt.Sprintf("--proto_path=%s", ip))
		}
	}
	if len(cfg.ImportPaths) == 0 {
		args = append(args, "--proto_path=.")
	}

	// Language-specific flags
	switch lang {
	case LanguageGo:
		args = append(args, fmt.Sprintf("--go_out=%s", outDir))
		args = append(args, "--go_opt=paths=source_relative")
		if cfg.GenerateGRPC {
			args = append(args, fmt.Sprintf("--go-grpc_out=%s", outDir))
			args = append(args, "--go-grpc_opt=paths=source_relative")
		}

	case LanguagePython:
		args = append(args, fmt.Sprintf("--python_out=%s", outDir))
		if cfg.GenerateGRPC {
			args = append(args, fmt.Sprintf("--grpc_python_out=%s", outDir))
		}

	case LanguageRust:
		args = append(args, fmt.Sprintf("--rust_out=%s", outDir))
		if cfg.GenerateGRPC {
			args = append(args, fmt.Sprintf("--grpc-rust_out=%s", outDir))
		}

	case LanguageTypeScript:
		args = append(args, fmt.Sprintf("--ts_proto_out=%s", outDir))
		args = append(args, "--ts_proto_opt=outputServices=grpc-js,env=node,esModuleInterop=true")

	case LanguageCpp:
		args = append(args, fmt.Sprintf("--cpp_out=%s", outDir))
		if cfg.GenerateGRPC {
			args = append(args, fmt.Sprintf("--grpc_out=%s", outDir))
		}

	default:
		return nil, fmt.Errorf("unsupported codegen target language: %s", lang)
	}

	// Append extra flags
	args = append(args, cfg.ExtraFlags...)

	// Append proto files
	args = append(args, cfg.ProtoFiles...)

	return &CodegenCommand{
		Executable: compiler,
		Args:       args,
		Language:   lang,
	}, nil
}

// CheckCompilerAvailable verifies if the compiler binary exists in PATH.
func CheckCompilerAvailable(executable string) (bool, string) {
	if executable == "" {
		executable = "protoc"
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return false, ""
	}
	return true, path
}

// GetMissingCompilerHelp produces actionable installation instructions when the compiler is missing.
func GetMissingCompilerHelp(language, executable string) string {
	if executable == "" {
		executable = "protoc"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Compiler '%s' was not found in PATH.\n\n", executable))
	sb.WriteString("To install Protocol Buffers compiler:\n")

	switch runtime.GOOS {
	case "windows":
		sb.WriteString("  • Windows (winget): winget install Google.Protobuf\n")
		sb.WriteString("  • Windows (choco):  choco install protoc\n")
		sb.WriteString("  • Windows (scoop):  scoop install protoc\n")
	case "darwin":
		sb.WriteString("  • macOS (Homebrew): brew install protobuf\n")
	default:
		sb.WriteString("  • Ubuntu/Debian:    sudo apt-get install -y protobuf-compiler\n")
		sb.WriteString("  • Arch Linux:       sudo pacman -S protobuf\n")
		sb.WriteString("  • Fedora/RHEL:      sudo dnf install -y protobuf-compiler\n")
	}

	sb.WriteString("\nLanguage-specific plugin requirements:\n")
	switch language {
	case LanguageGo:
		sb.WriteString("  • Go Protobuf: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest\n")
		sb.WriteString("  • Go gRPC:     go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest\n")
	case LanguagePython:
		sb.WriteString("  • Python gRPC: pip install grpcio-tools\n")
	case LanguageRust:
		sb.WriteString("  • Rust:        cargo install protobuf-codegen\n")
		sb.WriteString("                 Or use tonic-build in build.rs: cargo add tonic-build --build\n")
	case LanguageTypeScript:
		sb.WriteString("  • TypeScript:  npm install -g ts-proto grpc-tools\n")
	case LanguageCpp:
		sb.WriteString("  • C++ (vcpkg): vcpkg install protobuf grpc\n")
	}

	return sb.String()
}

// ExecuteCodegen compiles proto files into code or returns helpful diagnostics.
func ExecuteCodegen(ctx context.Context, cfg CodegenConfig) (*CodegenResult, error) {
	cmd, err := BuildCodegenCommand(cfg)
	if err != nil {
		return nil, err
	}

	res := &CodegenResult{
		Command:  cmd.CommandString(),
		Compiler: cmd.Executable,
	}

	// 1. Verify compiler availability
	available, _ := CheckCompilerAvailable(cmd.Executable)
	if !available {
		help := GetMissingCompilerHelp(cfg.Language, cmd.Executable)
		res.HelpGuide = help
		res.ExitCode = 127
		res.Stderr = fmt.Sprintf("Executable '%s' not found.\n\n%s", cmd.Executable, help)
		return res, fmt.Errorf("compiler '%s' not found in PATH", cmd.Executable)
	}

	// 2. Execute compiler process
	startTime := time.Now()
	execCmd := exec.CommandContext(ctx, cmd.Executable, cmd.Args...)

	var stdoutBuf, stderrBuf bytes.Buffer
	execCmd.Stdout = &stdoutBuf
	execCmd.Stderr = &stderrBuf

	runErr := execCmd.Run()
	res.Duration = time.Since(startTime)
	res.Stdout = stdoutBuf.String()
	res.Stderr = stderrBuf.String()

	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.ExitCode = 1
		}
		res.Success = false
		return res, fmt.Errorf("codegen failed with exit code %d: %s", res.ExitCode, res.Stderr)
	}

	res.Success = true
	res.ExitCode = 0
	return res, nil
}
