# sindoq (means "box" in Kurdish) 

```
       _         __
  ___ (_)__  ___/ /__  ___ _
 (_-</ / _ \/ _  / _ \/ _ `/
/___/_/_//_/\_,_/\___/\_, /
                       /_/
         AI Sandbox
```


**AI Sandbox** - One API, anywhere.

```
  ┌──────────────────────────────────────────────────────────────────────┐
  │                                                                      │
  │   ~ your-app                                                         │
  │   ──────────────────────────────────────────────────────────────     │
  │                                                                      │
  │   code := `                                                          │
  │     import pandas as pd                                              │
  │     df = pd.read_csv("data.csv")                                     │
  │     print(df.describe())                                             │
  │   `                                                                  │
  │                                                                      │
  │   result, _ := sindoq.Execute(ctx, code)                             │
  │                                                                      │
  └──────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
  ┌──────────────────────────────────────────────────────────────────────┐
  │  ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  │
  │  ░  sindoq                                                        ░  │
  │  ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  │
  │                                                                      │
  │   ▸ Detecting language...     Python ✓                               │
  │   ▸ Selecting provider...     Docker ✓                               │
  │   ▸ Creating sandbox...       container:a3f8c2d ✓                    │
  │   ▸ Executing code...         ████████████████████ 100%              │
  │                                                                      │
  └──────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
  ┌──────────────────────────────────────────────────────────────────────┐
  │                                                                      │
  │   ~ sandbox:a3f8c2d                                                  │
  │   ──────────────────────────────────────────────────────────────     │
  │                                                                      │
  │   $ python main.py                                                   │
  │                                                                      │
  │              count      mean       std   min   max                   │
  │   price      1000    45.230    12.450  10.0  99.0                    │
  │   quantity   1000   125.800    45.200  10.0  500.0                   │
  │                                                                      │
  │   ─────────────────────────────────────────────────────────────      │
  │   exit: 0  │  time: 127ms  │  mem: 45MB  │  cpu: 0.2s                │
  │                                                                      │
  └──────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
            ExecutionResult{ ExitCode: 0, Stdout: "...", Duration: 127ms }
```

## Why sindoq?

```
         ┌─────────────┐       ┌─────────────┐       ┌─────────────┐
         │ ░░░░░░░░░░░ │       │ ░░░░░░░░░░░ │       │ ░░░░░░░░░░░ │
         │ ░ Docker  ░ │       │ ░ Vercel  ░ │       │ ░ K8s     ░ │
         │ ░░░░░░░░░░░ │       │ ░░░░░░░░░░░ │       │ ░░░░░░░░░░░ │
         │             │       │             │       │             │
         │  Local      │       │  Cloud      │       │  Enterprise │
         │  Free       │       │  Managed    │       │  Self-host  │
         │  Fast       │       │  Scalable   │       │  Control    │
         └─────────────┘       └─────────────┘       └─────────────┘
                │                     │                     │
                └─────────────────────┼─────────────────────┘
                                      │
                                      ▼
                        ┌─────────────────────────┐
                        │                         │
                        │   Same code.            │
                        │   Same API.             │
                        │   Any provider.         │
                        │                         │
                        │   WithProvider("xxx")   │
                        │                         │
                        └─────────────────────────┘
```

## Features

- **Multi-provider support**: Docker, Podman, Wasmer, nsjail, gVisor, Firecracker, Landlock, Seatbelt, Kubernetes, Vercel, E2B
- **Auto language detection**: Automatically detects programming language from code
- **Streaming output**: Real-time stdout/stderr streaming
- **Async execution**: Non-blocking execution with channels
- **Resource limits**: Control CPU, memory, and execution time
- **File system access**: Read/write files in sandbox environments

## Installation

```bash
go get github.com/happyhackingspace/sindoq
```

## Quick Start

### One-liner Execution

```go
result, err := sindoq.Execute(ctx, `print("Hello, World!")`)
fmt.Println(result.Stdout) // Hello, World!
```

### With Specific Provider

```go
sb, err := sindoq.Create(ctx, sindoq.WithProvider("docker"))
if err != nil {
    log.Fatal(err)
}
defer sb.Stop(ctx)

result, err := sb.Execute(ctx, `console.log("Hello")`, sindoq.WithLanguage("JavaScript"))
fmt.Println(result.Stdout)
```

### Streaming Output

```go
sb, err := sindoq.Create(ctx, sindoq.WithProvider("docker"))
defer sb.Stop(ctx)

err = sb.ExecuteStream(ctx, code, func(e *executor.StreamEvent) error {
    if e.Type == executor.StreamStdout {
        fmt.Print(e.Data)
    }
    return nil
})
```

### Async Execution

```go
results, err := sb.ExecuteAsync(ctx, code)

// Do other work...

result := <-results
fmt.Println(result.Stdout)
```

## Providers

| Provider | Type | Use Case |
|----------|------|----------|
| `docker` | Local | Development, CI/CD |
| `podman` | Local | Rootless containers |
| `wasmer` | Local | Cross-platform WASM sandbox (Linux, macOS, Windows) |
| `nsjail` | Local | Ultra-fast process isolation (~5ms, Linux only) |
| `gvisor` | Local | Strong isolation, syscall filtering (Linux only) |
| `firecracker` | Local | Maximum isolation (microVMs, Linux only) |
| `landlock` | Local | Kernel-level sandboxing via Landlock LSM (Linux 5.13+) |
| `seatbelt` | Local | Kernel-level sandboxing via sandbox-exec (macOS) |
| `kubernetes` | Cloud | Scalable workloads |
| `vercel` | Cloud | Serverless execution |
| `e2b` | Cloud | AI code interpreter |

### Provider Configuration

```go
// Docker
sb, _ := sindoq.Create(ctx, sindoq.WithDockerConfig(sindoq.DockerConfig{
    Host: "unix:///var/run/docker.sock",
}))

// Vercel
sb, _ := sindoq.Create(ctx, sindoq.WithVercelConfig(sindoq.VercelConfig{
    Token: os.Getenv("VERCEL_TOKEN"),
}))

// E2B
sb, _ := sindoq.Create(ctx, sindoq.WithE2BConfig(sindoq.E2BConfig{
    APIKey: os.Getenv("E2B_API_KEY"),
}))

// Wasmer (cross-platform)
sb, _ := sindoq.Create(ctx, sindoq.WithWasmerConfig(sindoq.WasmerConfig{
    WasmerPath: "wasmer",
    TimeLimit:  30,
}))

// Seatbelt (macOS)
sb, _ := sindoq.Create(ctx, sindoq.WithSeatbeltConfig(sindoq.SeatbeltConfig{
    EnableNetwork: false,
}))

// Landlock (Linux 5.13+)
sb, _ := sindoq.Create(ctx, sindoq.WithLandlockConfig(sindoq.LandlockConfig{
    BestEffort: true,
}))
```

## Configuration Options

### Sandbox Options

```go
sb, _ := sindoq.Create(ctx,
    sindoq.WithProvider("docker"),
    sindoq.WithRuntime("Python"),
    sindoq.WithTimeout(5*time.Minute),
    sindoq.WithResources(sindoq.ResourceConfig{
        MemoryMB: 512,
        CPUs:     2,
        DiskMB:   1024,
    }),
    sindoq.WithInternetAccess(),
)
```

### Execution Options

```go
result, _ := sb.Execute(ctx, code,
    sindoq.WithLanguage("Python"),
    sindoq.WithExecutionTimeout(30*time.Second),
    sindoq.WithEnv(map[string]string{"DEBUG": "true"}),
    sindoq.WithStdin("input data"),
    sindoq.WithWorkDir("/app"),
)
```

## Supported Languages

| Language | Runtime | Docker Image |
|----------|---------|--------------|
| Python | python3 | python:3.11-slim |
| JavaScript | node | node:20-slim |
| TypeScript | ts-node | node:20-slim |
| Go | go run | golang:1.25-alpine |
| Rust | rustc | rust:1.75-slim |
| Java | java | eclipse-temurin:21 |
| C | gcc | gcc:13 |
| C++ | g++ | gcc:13 |
| Ruby | ruby | ruby:3.3-slim |
| PHP | php | php:8.3-cli |
| Shell | bash | alpine:3.19 |

## CLI Usage

```bash
# Install CLI
go install github.com/happyhackingspace/sindoq/cmd/sindoq@latest

# Execute code
sindoq 'print("Hello")'

# Specify language
sindoq -lang javascript 'console.log("Hi")'

# Execute from file
sindoq -file script.py

# Stream output
sindoq -stream 'for i in range(10): print(i)'

# Use different provider
sindoq -provider vercel 'print("Hello")'

# Use Wasmer (works on Linux, macOS, Windows)
sindoq -provider wasmer 'print("Hello from WASM!")'
sindoq -provider wasmer -lang javascript 'console.log("Hello from QuickJS!")'

# Use Seatbelt (macOS native sandboxing)
sindoq -provider seatbelt 'print("Hello from Seatbelt!")'

# Use Landlock (Linux native sandboxing, kernel 5.13+)
sindoq -provider landlock 'print("Hello from Landlock!")'

# Pipe input
echo 'puts "Hello"' | sindoq -lang ruby

# Detect language only
sindoq -detect 'fn main() { println!("Hello"); }'

# List supported languages
sindoq -list-languages
```

## API Reference

### Sandbox Interface

```go
type Sandbox interface {
    ID() string
    Provider() string
    Execute(ctx context.Context, code string, opts ...ExecuteOption) (*ExecutionResult, error)
    ExecuteAsync(ctx context.Context, code string, opts ...ExecuteOption) (<-chan *ExecutionResult, error)
    ExecuteStream(ctx context.Context, code string, handler StreamHandler, opts ...ExecuteOption) error
    RunCommand(ctx context.Context, cmd string, args ...string) (*CommandResult, error)
    Files() FileSystem
    Stop(ctx context.Context) error
    Status(ctx context.Context) (SandboxStatus, error)
}
```

### ExecutionResult

```go
type ExecutionResult struct {
    ExitCode  int
    Stdout    string
    Stderr    string
    Duration  time.Duration
    Language  string
    Artifacts []Artifact
}
```

## Use Cases

```
┌────────────────────────────────────────────────────────────────────────────┐
│                                                                            │
│   ▸ Coding Agents                                                          │
│     Execute AI-generated code, run terminal commands, access filesystem    │
│                                                                            │
│   ▸ AI Data Analysis                                                       │
│     Securely explore datasets and generate visualizations                  │
│                                                                            │
│   ▸ Code Interpreters                                                      │
│     Build ChatGPT-like code execution for your AI assistant                │
│                                                                            │
│   ▸ Deep Research Agents                                                   │
│     Long-running analysis on large datasets with streaming results         │
│                                                                            │
│   ▸ Automation Agents                                                      │
│     Workflow automation with real code execution capabilities              │
│                                                                            │
│   ▸ Interview Platforms                                                    │
│     Run candidate code safely with resource limits                         │
│                                                                            │
│   ▸ Education & Grading                                                    │
│     Auto-grade student submissions in isolated environments                │
│                                                                            │
└────────────────────────────────────────────────────────────────────────────┘
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `VERCEL_TOKEN` | Vercel API token |
| `E2B_API_KEY` | E2B API key |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT

---

<p align="center">
  Made with ♥ by <a href="https://github.com/HappyHackingSpace">Happy Hacking Space</a>
</p>
