//go:build microsandbox && cgo

package microsandbox

import (
	"strings"
	"testing"
	"time"

	msb "github.com/superradcompany/microsandbox/sdk/go"

	"github.com/happyhackingspace/sindoq/internal/factory"
	"github.com/happyhackingspace/sindoq/internal/provider"
	"github.com/happyhackingspace/sindoq/pkg/executor"
)

func applyOptions(opts []msb.SandboxOption) msb.SandboxConfig {
	var cfg msb.SandboxConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

func TestResolveImage(t *testing.T) {
	tests := []struct {
		name string
		opts provider.CreateOptions
		def  string
		want string
	}{
		{"explicit image wins", provider.CreateOptions{Image: "alpine:3.19", Runtime: "Python"}, "x", "alpine:3.19"},
		{"runtime image", provider.CreateOptions{Runtime: "JavaScript"}, "x", "node:22-slim"},
		{"runtime alias", provider.CreateOptions{Runtime: "py"}, "x", "python:3.12-slim"},
		{"unknown runtime falls back", provider.CreateOptions{Runtime: "NoSuchLanguage"}, "busybox", "busybox"},
		{"provider default", provider.CreateOptions{}, "busybox", "busybox"},
		{"empty default", provider.CreateOptions{}, "", "python:3.12-slim"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveImage(&tt.opts, tt.def); got != tt.want {
				t.Errorf("resolveImage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVCPUCount(t *testing.T) {
	tests := []struct {
		in   float64
		want uint8
	}{
		{0.5, 1},
		{1, 1},
		{1.2, 2},
		{4, 4},
		{1000, 255},
	}
	for _, tt := range tests {
		if got := vcpuCount(tt.in); got != tt.want {
			t.Errorf("vcpuCount(%v) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestSandboxOptions(t *testing.T) {
	cfg := applyOptions(sandboxOptions(&provider.CreateOptions{
		Runtime:     "Python",
		Resources:   provider.ResourceConfig{MemoryMB: 256, CPUs: 1.5},
		Environment: map[string]string{"A": "1"},
		Labels:      map[string]string{"app": "sindoq"},
		Timeout:     90 * time.Second,
	}, "busybox"))

	if cfg.Image != "python:3.12-slim" {
		t.Errorf("Image = %q", cfg.Image)
	}
	if !cfg.Ephemeral {
		t.Error("sandbox should be ephemeral")
	}
	if cfg.MemoryMiB != 256 || cfg.CPUs != 2 {
		t.Errorf("MemoryMiB = %d, CPUs = %d; want 256, 2", cfg.MemoryMiB, cfg.CPUs)
	}
	if cfg.Env["A"] != "1" || cfg.Labels["app"] != "sindoq" {
		t.Errorf("Env = %v, Labels = %v", cfg.Env, cfg.Labels)
	}
	// sindoq passes the default execution timeout here, so it must not cap the
	// sandbox lifetime (a default sandbox would die after 30s).
	if cfg.MaxDuration != 0 {
		t.Errorf("Timeout must not become a sandbox lifetime cap, got MaxDuration = %v", cfg.MaxDuration)
	}
	if cfg.Network == nil || cfg.Network.DefaultEgress != msb.PolicyActionDeny {
		t.Errorf("InternetAccess=false should deny egress, got %+v", cfg.Network)
	}

	open := applyOptions(sandboxOptions(&provider.CreateOptions{InternetAccess: true}, "busybox"))
	if open.Network != nil {
		t.Errorf("InternetAccess=true should keep the default network, got %+v", open.Network)
	}
	if open.MemoryMiB != 0 || open.CPUs != 0 || open.MaxDuration != 0 {
		t.Error("zero resources/timeout should leave runtime defaults")
	}
}

func TestMapStatus(t *testing.T) {
	tests := map[msb.SandboxStatus]provider.InstanceStatus{
		msb.SandboxStatusCreated:  provider.StatusCreating,
		msb.SandboxStatusStarting: provider.StatusCreating,
		msb.SandboxStatusRunning:  provider.StatusRunning,
		msb.SandboxStatusDraining: provider.StatusRunning,
		msb.SandboxStatusPaused:   provider.StatusPaused,
		msb.SandboxStatusStopped:  provider.StatusStopped,
		msb.SandboxStatusCrashed:  provider.StatusError,
	}
	for in, want := range tests {
		if got := mapStatus(in); got != want {
			t.Errorf("mapStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExecWorkDir(t *testing.T) {
	i := &Instance{workDir: "/workspace"}
	if got := i.execWorkDir(&executor.ExecutionOptions{WorkDir: "/app"}); got != "/app" {
		t.Errorf("exec workdir should win, got %q", got)
	}
	if got := i.execWorkDir(&executor.ExecutionOptions{}); got != "/workspace" {
		t.Errorf("instance workdir should be used, got %q", got)
	}
	if got := (&Instance{}).execWorkDir(&executor.ExecutionOptions{}); got != "/tmp" {
		t.Errorf("fallback should be /tmp, got %q", got)
	}
}

func TestNewSandboxName(t *testing.T) {
	a, err := newSandboxName()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := newSandboxName()
	if !strings.HasPrefix(a, "sindoq-") || a == b {
		t.Errorf("names should be prefixed and unique: %q %q", a, b)
	}
}

func TestRegistration(t *testing.T) {
	if !factory.IsRegistered("microsandbox") {
		t.Fatal("microsandbox provider should be registered")
	}

	constructor, _ := factory.DefaultRegistry.GetConstructor("microsandbox")
	if _, err := constructor(Config{}); err == nil {
		t.Error("non-pointer config should be rejected")
	}

	p, err := constructor(&Config{DefaultImage: "busybox"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "microsandbox" {
		t.Errorf("Name() = %q", p.Name())
	}
	caps := p.Capabilities()
	if !caps.SupportsStreaming || !caps.SupportsFileSystem || caps.SupportsNetwork {
		t.Errorf("unexpected capabilities: %+v", caps)
	}
}
