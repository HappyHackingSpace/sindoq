package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/happyhackingspace/sindoq"
	"github.com/happyhackingspace/sindoq/pkg/executor"
	"github.com/happyhackingspace/sindoq/pkg/langdetect"

	_ "github.com/happyhackingspace/sindoq/internal/provider/docker"
	_ "github.com/happyhackingspace/sindoq/internal/provider/e2b"
	_ "github.com/happyhackingspace/sindoq/internal/provider/kubernetes"
	_ "github.com/happyhackingspace/sindoq/internal/provider/podman"
	_ "github.com/happyhackingspace/sindoq/internal/provider/vercel"
	_ "github.com/happyhackingspace/sindoq/internal/provider/wasmer"
	// nsjail, gvisor, and firecracker are imported in providers_linux.go (Linux only)
)

type outputFormat string

const (
	outputText outputFormat = "text"
	outputJSON outputFormat = "json"
)

type ExecutionOutput struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"duration_ms"`
	Language   string `json:"language"`
}

// Go's flag package stops parsing at first non-flag arg, so we reorder to allow flags anywhere
func reorderArgs() {
	args := os.Args[1:]
	var flags, positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flagName := strings.TrimLeft(arg, "-")
				if flagName == "provider" || flagName == "lang" || flagName == "timeout" || flagName == "file" {
					i++
					flags = append(flags, args[i])
				}
			}
		} else {
			positional = append(positional, arg)
		}
	}

	os.Args = append([]string{os.Args[0]}, append(flags, positional...)...)
}

func main() {
	provider := flag.String("provider", "docker", "Provider to use (docker, podman, wasmer, nsjail, gvisor, firecracker, kubernetes, vercel, e2b)")
	language := flag.String("lang", "", "Language (auto-detected if not specified)")
	timeout := flag.Duration("timeout", 5*time.Minute, "Execution timeout")
	stream := flag.Bool("stream", false, "Stream output in real-time")
	jsonFormat := flag.Bool("json", false, "Output in JSON format")
	file := flag.String("file", "", "Execute code from file")
	detect := flag.Bool("detect", false, "Only detect language, don't execute")
	listLangs := flag.Bool("list-languages", false, "List supported languages")
	version := flag.Bool("version", false, "Show version")

	reorderArgs()

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `sindoq - Code execution across multiple isolated environments

Usage:
  sindoq [flags] [code]
  sindoq [flags] -file <filename>
  echo "print('hello')" | sindoq [flags]

Flags:
`)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  sindoq 'print("Hello")'
  sindoq --json 'print("Hello")'
  sindoq -file script.py
  sindoq 'console.log("Hi")' -lang javascript
  sindoq -stream 'for i in range(5): print(i)'
  echo 'puts "Hello"' | sindoq -lang ruby

Environment Variables:
  VERCEL_TOKEN     - Vercel API token
  E2B_API_KEY      - E2B API key
`)
	}

	flag.Parse()

	if *version {
		fmt.Println("sindoq version 0.1.0")
		return
	}

	if *listLangs {
		listLanguages()
		return
	}

	code, err := getCode(flag.Args(), *file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if code == "" {
		flag.Usage()
		os.Exit(1)
	}

	if *detect {
		detectLanguage(code)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	format := outputText
	if *jsonFormat {
		format = outputJSON
	}
	if err := executeCode(ctx, code, *provider, *language, *stream, format); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func getCode(args []string, filename string) (string, error) {
	if filename != "" {
		data, err := os.ReadFile(filename)
		if err != nil {
			return "", fmt.Errorf("reading file: %w", err)
		}
		return string(data), nil
	}

	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}

	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	return "", nil
}

func detectLanguage(code string) {
	detector := langdetect.New()
	result := detector.Detect(code, langdetect.DefaultDetectOptions())

	fmt.Printf("Language:   %s\n", result.Language)
	fmt.Printf("Confidence: %.2f\n", result.Confidence)
	fmt.Printf("Method:     %s\n", result.Method)

	if info, ok := langdetect.GetRuntimeInfo(result.Language); ok {
		fmt.Printf("Runtime:    %s\n", info.Runtime)
		fmt.Printf("Extension:  %s\n", info.FileExt)
		fmt.Printf("Docker:     %s\n", info.DockerImage)
	}
}

func listLanguages() {
	fmt.Println("Supported Languages:")
	fmt.Println()
	fmt.Printf("%-15s %-10s %-15s %s\n", "LANGUAGE", "EXT", "RUNTIME", "DOCKER IMAGE")
	fmt.Printf("%-15s %-10s %-15s %s\n", "--------", "---", "-------", "------------")

	for _, lang := range langdetect.SupportedLanguages() {
		info, _ := langdetect.GetRuntimeInfo(lang)
		fmt.Printf("%-15s %-10s %-15s %s\n", lang, info.FileExt, info.Runtime, info.DockerImage)
	}
}

func executeCode(ctx context.Context, code, providerName, language string, stream bool, format outputFormat) error {
	if language == "" {
		detector := langdetect.New()
		result := detector.Detect(code, langdetect.DefaultDetectOptions())
		if result.Language != "" {
			language = result.Language
		} else {
			language = "Python"
		}
	}

	opts := []sindoq.Option{
		sindoq.WithProvider(providerName),
		sindoq.WithRuntime(language),
	}

	switch providerName {
	case "vercel":
		if token := os.Getenv("VERCEL_TOKEN"); token != "" {
			opts = append(opts, sindoq.WithVercelConfig(sindoq.VercelConfig{Token: token}))
		}
	case "e2b":
		if key := os.Getenv("E2B_API_KEY"); key != "" {
			opts = append(opts, sindoq.WithE2BConfig(sindoq.E2BConfig{APIKey: key}))
		}
	}

	sb, err := sindoq.Create(ctx, opts...)
	if err != nil {
		return fmt.Errorf("creating sandbox: %w", err)
	}
	defer sb.Stop(ctx)

	var execOpts []sindoq.ExecuteOption
	if language != "" {
		execOpts = append(execOpts, sindoq.WithLanguage(language))
	}

	if stream && format != outputJSON {
		return sb.ExecuteStream(ctx, code, func(e *executor.StreamEvent) error {
			switch e.Type {
			case executor.StreamStdout:
				fmt.Print(e.Data)
			case executor.StreamStderr:
				fmt.Fprint(os.Stderr, e.Data)
			case executor.StreamError:
				return e.Error
			}
			return nil
		}, execOpts...)
	}

	start := time.Now()
	result, err := sb.Execute(ctx, code, execOpts...)
	if err != nil {
		return err
	}

	output := ExecutionOutput{
		ExitCode:   result.ExitCode,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		DurationMs: time.Since(start).Milliseconds(),
		Language:   language,
	}

	if err := writeOutput(output, format); err != nil {
		return err
	}

	if result.ExitCode != 0 {
		os.Exit(result.ExitCode)
	}

	return nil
}

func writeOutput(output ExecutionOutput, format outputFormat) error {
	switch format {
	case outputJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	default:
		if output.Stdout != "" {
			fmt.Print(output.Stdout)
		}
		if output.Stderr != "" {
			fmt.Fprint(os.Stderr, output.Stderr)
		}
		return nil
	}
}
