package sindoq

import (
	"io"
	"time"

	"github.com/happyhackingspace/sindoq/internal/provider"
	"github.com/happyhackingspace/sindoq/pkg/event"
)

// Config holds sandbox configuration.
type Config struct {
	// Provider specifies which provider to use.
	Provider string

	// ProviderConfig holds provider-specific configuration.
	ProviderConfig any

	// DefaultTimeout for execution.
	DefaultTimeout time.Duration

	// DefaultLanguage when detection fails.
	DefaultLanguage string

	// Runtime specifies the language runtime for sandbox creation (e.g., "Python", "JavaScript").
	// This determines which Docker image or runtime environment to use.
	Runtime string

	// Image specifies a specific container/VM image to use (overrides Runtime).
	Image string

	// Resources configuration.
	Resources ResourceConfig

	// Logger for debug output.
	Logger Logger

	// EventHandler for global events.
	EventHandler event.EventHandler

	// AutoDetectLanguage enables automatic language detection.
	AutoDetectLanguage bool

	// InternetAccess controls network access from sandbox.
	InternetAccess bool
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Provider:           "docker",
		DefaultTimeout:     30 * time.Second,
		AutoDetectLanguage: true,
		InternetAccess:     false,
		Resources: ResourceConfig{
			MemoryMB: 512,
			CPUs:     1,
			DiskMB:   1024,
		},
	}
}

// Option configures a sandbox.
type Option func(*Config)

// WithProvider sets the provider.
func WithProvider(providerName string) Option {
	return func(c *Config) {
		c.Provider = providerName
	}
}

// WithRuntime sets the language runtime for sandbox creation.
// This determines which Docker image or runtime environment to use.
// Examples: "Python", "JavaScript", "Go", "Rust"
func WithRuntime(runtime string) Option {
	return func(c *Config) {
		c.Runtime = runtime
	}
}

// WithImage sets a specific container/VM image to use.
// This overrides the automatic image selection based on Runtime.
func WithImage(image string) Option {
	return func(c *Config) {
		c.Image = image
	}
}

// WithDockerConfig configures Docker provider.
func WithDockerConfig(cfg DockerConfig) Option {
	return func(c *Config) {
		c.Provider = "docker"
		c.ProviderConfig = cfg
	}
}

// WithVercelConfig configures Vercel Sandbox provider.
func WithVercelConfig(cfg VercelConfig) Option {
	return func(c *Config) {
		c.Provider = "vercel"
		c.ProviderConfig = cfg
	}
}

// WithE2BConfig configures E2B provider.
func WithE2BConfig(cfg E2BConfig) Option {
	return func(c *Config) {
		c.Provider = "e2b"
		c.ProviderConfig = cfg
	}
}

// WithKubernetesConfig configures Kubernetes provider.
func WithKubernetesConfig(cfg KubernetesConfig) Option {
	return func(c *Config) {
		c.Provider = "kubernetes"
		c.ProviderConfig = cfg
	}
}

// WithPodmanConfig configures Podman provider.
func WithPodmanConfig(cfg PodmanConfig) Option {
	return func(c *Config) {
		c.Provider = "podman"
		c.ProviderConfig = cfg
	}
}

// WithFirecrackerConfig configures Firecracker provider.
func WithFirecrackerConfig(cfg FirecrackerConfig) Option {
	return func(c *Config) {
		c.Provider = "firecracker"
		c.ProviderConfig = cfg
	}
}

// WithGVisorConfig configures gVisor provider.
func WithGVisorConfig(cfg GVisorConfig) Option {
	return func(c *Config) {
		c.Provider = "gvisor"
		c.ProviderConfig = cfg
	}
}

// WithNsjailConfig configures nsjail provider.
func WithNsjailConfig(cfg NsjailConfig) Option {
	return func(c *Config) {
		c.Provider = "nsjail"
		c.ProviderConfig = cfg
	}
}

// WithWasmerConfig configures Wasmer WebAssembly provider.
func WithWasmerConfig(cfg WasmerConfig) Option {
	return func(c *Config) {
		c.Provider = "wasmer"
		c.ProviderConfig = cfg
	}
}

// WithLandlockConfig configures Landlock provider.
func WithLandlockConfig(cfg LandlockConfig) Option {
	return func(c *Config) {
		c.Provider = "landlock"
		c.ProviderConfig = cfg
	}
}

// WithSeatbeltConfig configures Seatbelt provider.
func WithSeatbeltConfig(cfg SeatbeltConfig) Option {
	return func(c *Config) {
		c.Provider = "seatbelt"
		c.ProviderConfig = cfg
	}
}

// WithTimeout sets the default execution timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.DefaultTimeout = d
	}
}

// WithResources sets resource limits.
func WithResources(r ResourceConfig) Option {
	return func(c *Config) {
		c.Resources = r
	}
}

// WithLogger sets a custom logger.
func WithLogger(l Logger) Option {
	return func(c *Config) {
		c.Logger = l
	}
}

// WithEventHandler sets a global event handler.
func WithEventHandler(h event.EventHandler) Option {
	return func(c *Config) {
		c.EventHandler = h
	}
}

// WithAutoDetect enables automatic language detection.
func WithAutoDetect() Option {
	return func(c *Config) {
		c.AutoDetectLanguage = true
	}
}

// WithInternetAccess enables network access from sandbox.
func WithInternetAccess() Option {
	return func(c *Config) {
		c.InternetAccess = true
	}
}

// ResourceConfig defines resource limits.
type ResourceConfig struct {
	MemoryMB int
	CPUs     float64
	DiskMB   int
}

// ToProviderConfig converts to provider.ResourceConfig.
func (r ResourceConfig) ToProviderConfig() provider.ResourceConfig {
	return provider.ResourceConfig{
		MemoryMB: r.MemoryMB,
		CPUs:     r.CPUs,
		DiskMB:   r.DiskMB,
	}
}

// Logger interface for debug output.
type Logger interface {
	Debug(msg string, keysAndValues ...any)
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// NopLogger is a no-op logger.
type NopLogger struct{}

func (NopLogger) Debug(msg string, keysAndValues ...any) {}
func (NopLogger) Info(msg string, keysAndValues ...any)  {}
func (NopLogger) Warn(msg string, keysAndValues ...any)  {}
func (NopLogger) Error(msg string, keysAndValues ...any) {}

// WriterLogger logs to an io.Writer.
type WriterLogger struct {
	out io.Writer
}

func NewWriterLogger(w io.Writer) *WriterLogger {
	return &WriterLogger{out: w}
}

func (l *WriterLogger) Debug(msg string, keysAndValues ...any) {}
func (l *WriterLogger) Info(msg string, keysAndValues ...any)  {}
func (l *WriterLogger) Warn(msg string, keysAndValues ...any)  {}
func (l *WriterLogger) Error(msg string, keysAndValues ...any) {}

// Provider-specific configurations

// DockerConfig configures Docker provider.
type DockerConfig struct {
	Host         string
	APIVersion   string
	TLSVerify    bool
	CertPath     string
	RegistryAuth map[string]string
	DefaultImage string
}

// VercelConfig configures Vercel Sandbox provider.
type VercelConfig struct {
	Token     string
	TeamID    string
	ProjectID string
	Runtime   string // "node22" or "python313"
}

// E2BConfig configures E2B provider.
type E2BConfig struct {
	APIKey   string
	Template string
	Timeout  time.Duration
}

// KubernetesConfig configures Kubernetes provider.
type KubernetesConfig struct {
	KubeConfig     string
	Namespace      string
	Image          string
	ServiceAccount string
	PodTemplate    string // Optional custom pod template
}

// PodmanConfig configures Podman provider.
type PodmanConfig struct {
	URI      string // unix:///run/podman/podman.sock
	Identity string
}

// FirecrackerConfig configures Firecracker provider.
// Firecracker requires Linux with KVM support.
type FirecrackerConfig struct {
	// FirecrackerBinary is the path to the firecracker binary.
	FirecrackerBinary string

	// KernelImagePath is the path to the uncompressed Linux kernel image.
	KernelImagePath string

	// RootDrivePath is the path to the root filesystem image.
	RootDrivePath string

	// VCPUCount is the number of vCPUs for the VM.
	VCPUCount int64

	// MemSizeMiB is the memory size in MiB.
	MemSizeMiB int64

	// SocketDir is the directory for VM sockets.
	SocketDir string

	// EnableNetwork enables networking via TAP device.
	EnableNetwork bool

	// SSHKeyPath is the path to SSH key for VM access.
	SSHKeyPath string

	// VMIPAddress is the IP address assigned to the VM.
	VMIPAddress string
}

// GVisorConfig configures gVisor provider.
// gVisor requires Linux and runsc to be installed.
type GVisorConfig struct {
	// RuntimeName is the Docker runtime name (default: "runsc").
	RuntimeName string

	// RuntimePath is the path to runsc binary.
	RuntimePath string

	// Platform specifies gVisor platform: "ptrace" or "kvm".
	Platform string

	// Network specifies network mode: "sandbox" or "host".
	Network string

	// DockerHost is the Docker daemon socket.
	DockerHost string

	// DefaultImage is the default container image.
	DefaultImage string

	// Debug enables gVisor debug logging.
	Debug bool
}

// NsjailConfig configures nsjail provider.
// nsjail requires Linux and the nsjail binary to be installed.
type NsjailConfig struct {
	// NsjailPath is the path to nsjail binary.
	NsjailPath string

	// Chroot is the chroot directory (default: "/" for host root).
	Chroot string

	// User and Group IDs for the sandboxed process.
	User  uint32
	Group uint32

	// TimeLimit is the maximum execution time in seconds.
	TimeLimit uint32

	// MaxMemoryMB is the memory limit in megabytes.
	MaxMemoryMB uint32

	// MaxCPUs limits CPU cores.
	MaxCPUs uint32

	// EnableNetwork allows network access.
	EnableNetwork bool

	// ReadOnlyBindMounts are paths to bind-mount read-only.
	ReadOnlyBindMounts []string
}

// WasmerConfig configures Wasmer WebAssembly provider.
// Wasmer is cross-platform (Linux, macOS, Windows).
type WasmerConfig struct {
	// WasmerPath is the path to wasmer binary.
	WasmerPath string

	// CacheDir is the directory for WASM module cache.
	CacheDir string

	// TimeLimit is the maximum execution time in seconds.
	TimeLimit uint32

	// MaxMemoryMB is the memory limit in megabytes.
	MaxMemoryMB uint32

	// EnableNetwork allows network access via WASI.
	EnableNetwork bool
}

// LandlockConfig configures Landlock provider.
// Landlock requires Linux with kernel 5.13+.
type LandlockConfig struct {
	// ABIVersion is the Landlock ABI version to target.
	ABIVersion int

	// BestEffort degrades gracefully on older kernels.
	BestEffort bool

	// IgnoreIfMissing skips paths that don't exist.
	IgnoreIfMissing bool

	// TimeLimit is the maximum execution time in seconds.
	TimeLimit uint32

	// ReadPaths are paths to allow read access (files).
	ReadPaths []string

	// ReadExecPaths are paths to allow read and execute access (directories).
	ReadExecPaths []string

	// WritePaths are paths to allow read-write access (directories).
	WritePaths []string

	// WriteExecPaths are paths to allow read-write-execute access (directories).
	WriteExecPaths []string

	// NetworkConnectPorts are TCP ports allowed for outbound connections.
	NetworkConnectPorts []int

	// NetworkBindPorts are TCP ports allowed for binding.
	NetworkBindPorts []int

	// EnableNetwork allows all network access.
	EnableNetwork bool
}

// SeatbeltConfig configures Seatbelt provider.
// Seatbelt requires macOS 10.5+.
type SeatbeltConfig struct {
	// TimeLimit is the maximum execution time in seconds.
	TimeLimit uint32

	// ReadPaths are paths to allow read access.
	ReadPaths []string

	// ReadExecPaths are paths to allow read and execute access.
	ReadExecPaths []string

	// WritePaths are paths to allow write access.
	WritePaths []string

	// EnableNetwork allows network access.
	EnableNetwork bool

	// AllowMachLookup allows Mach IPC lookups for essential services.
	AllowMachLookup bool

	// CustomProfile is an optional full SBPL profile string that overrides generation.
	CustomProfile string
}

// ExecuteOption configures a single execution.
type ExecuteOption func(*ExecuteConfig)

// ExecuteConfig holds execution configuration.
type ExecuteConfig struct {
	Language      string
	Filename      string
	Timeout       time.Duration
	Env           map[string]string
	WorkDir       string
	Stdin         string
	Files         map[string][]byte
	KeepArtifacts bool
}

// DefaultExecuteConfig returns default execution config.
func DefaultExecuteConfig() *ExecuteConfig {
	return &ExecuteConfig{
		Timeout: 30 * time.Second,
		WorkDir: "/workspace",
		Env:     make(map[string]string),
		Files:   make(map[string][]byte),
	}
}

// WithLanguage overrides automatic language detection.
func WithLanguage(lang string) ExecuteOption {
	return func(c *ExecuteConfig) {
		c.Language = lang
	}
}

// WithFilename provides filename hint for detection.
func WithFilename(name string) ExecuteOption {
	return func(c *ExecuteConfig) {
		c.Filename = name
	}
}

// WithExecutionTimeout sets execution timeout.
func WithExecutionTimeout(d time.Duration) ExecuteOption {
	return func(c *ExecuteConfig) {
		c.Timeout = d
	}
}

// WithEnv sets environment variables.
func WithEnv(env map[string]string) ExecuteOption {
	return func(c *ExecuteConfig) {
		c.Env = env
	}
}

// WithStdin provides standard input.
func WithStdin(input string) ExecuteOption {
	return func(c *ExecuteConfig) {
		c.Stdin = input
	}
}

// WithFiles adds files to the execution environment.
func WithFiles(files map[string][]byte) ExecuteOption {
	return func(c *ExecuteConfig) {
		c.Files = files
	}
}

// WithWorkDir sets the working directory.
func WithWorkDir(dir string) ExecuteOption {
	return func(c *ExecuteConfig) {
		c.WorkDir = dir
	}
}

// WithKeepArtifacts preserves generated files after execution.
func WithKeepArtifacts() ExecuteOption {
	return func(c *ExecuteConfig) {
		c.KeepArtifacts = true
	}
}
