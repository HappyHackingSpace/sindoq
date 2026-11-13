package sindoq

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Provider != "docker" {
		t.Errorf("Provider = %q, want %q", cfg.Provider, "docker")
	}
	if cfg.DefaultTimeout != 30*time.Second {
		t.Errorf("DefaultTimeout = %v, want %v", cfg.DefaultTimeout, 30*time.Second)
	}
	if !cfg.AutoDetectLanguage {
		t.Error("AutoDetectLanguage should be true by default")
	}
	if cfg.InternetAccess {
		t.Error("InternetAccess should be false by default")
	}
	if cfg.Resources.MemoryMB != 512 {
		t.Errorf("Resources.MemoryMB = %d, want 512", cfg.Resources.MemoryMB)
	}
	if cfg.Resources.CPUs != 1 {
		t.Errorf("Resources.CPUs = %f, want 1", cfg.Resources.CPUs)
	}
	if cfg.Resources.DiskMB != 1024 {
		t.Errorf("Resources.DiskMB = %d, want 1024", cfg.Resources.DiskMB)
	}
}

func TestWithProvider(t *testing.T) {
	cfg := DefaultConfig()
	WithProvider("vercel")(cfg)

	if cfg.Provider != "vercel" {
		t.Errorf("Provider = %q, want %q", cfg.Provider, "vercel")
	}
}

func TestWithRuntime(t *testing.T) {
	cfg := DefaultConfig()
	WithRuntime("Python")(cfg)

	if cfg.Runtime != "Python" {
		t.Errorf("Runtime = %q, want %q", cfg.Runtime, "Python")
	}
}

func TestWithImage(t *testing.T) {
	cfg := DefaultConfig()
	WithImage("python:3.11-slim")(cfg)

	if cfg.Image != "python:3.11-slim" {
		t.Errorf("Image = %q, want %q", cfg.Image, "python:3.11-slim")
	}
}

func TestWithTimeout(t *testing.T) {
	cfg := DefaultConfig()
	WithTimeout(5 * time.Minute)(cfg)

	if cfg.DefaultTimeout != 5*time.Minute {
		t.Errorf("DefaultTimeout = %v, want %v", cfg.DefaultTimeout, 5*time.Minute)
	}
}

func TestWithResources(t *testing.T) {
	cfg := DefaultConfig()
	res := ResourceConfig{
		MemoryMB: 1024,
		CPUs:     2,
		DiskMB:   2048,
	}
	WithResources(res)(cfg)

	if cfg.Resources.MemoryMB != 1024 {
		t.Errorf("Resources.MemoryMB = %d, want 1024", cfg.Resources.MemoryMB)
	}
	if cfg.Resources.CPUs != 2 {
		t.Errorf("Resources.CPUs = %f, want 2", cfg.Resources.CPUs)
	}
	if cfg.Resources.DiskMB != 2048 {
		t.Errorf("Resources.DiskMB = %d, want 2048", cfg.Resources.DiskMB)
	}
}

func TestWithAutoDetect(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoDetectLanguage = false
	WithAutoDetect()(cfg)

	if !cfg.AutoDetectLanguage {
		t.Error("AutoDetectLanguage should be true after WithAutoDetect")
	}
}

func TestWithInternetAccess(t *testing.T) {
	cfg := DefaultConfig()
	WithInternetAccess()(cfg)

	if !cfg.InternetAccess {
		t.Error("InternetAccess should be true after WithInternetAccess")
	}
}

func TestWithDockerConfig(t *testing.T) {
	cfg := DefaultConfig()
	dockerCfg := DockerConfig{
		Host:         "tcp://localhost:2375",
		DefaultImage: "python:3.11",
	}
	WithDockerConfig(dockerCfg)(cfg)

	if cfg.Provider != "docker" {
		t.Errorf("Provider = %q, want %q", cfg.Provider, "docker")
	}
	dc, ok := cfg.ProviderConfig.(DockerConfig)
	if !ok {
		t.Fatal("ProviderConfig should be DockerConfig")
	}
	if dc.Host != "tcp://localhost:2375" {
		t.Errorf("DockerConfig.Host = %q, want %q", dc.Host, "tcp://localhost:2375")
	}
}

func TestWithMicrosandboxConfig(t *testing.T) {
	cfg := DefaultConfig()
	WithMicrosandboxConfig(MicrosandboxConfig{DefaultImage: "alpine:3.19"})(cfg)

	if cfg.Provider != "microsandbox" {
		t.Errorf("Provider = %q, want %q", cfg.Provider, "microsandbox")
	}
	mc, ok := cfg.ProviderConfig.(MicrosandboxConfig)
	if !ok {
		t.Fatal("ProviderConfig should be MicrosandboxConfig")
	}
	if mc.DefaultImage != "alpine:3.19" {
		t.Errorf("MicrosandboxConfig.DefaultImage = %q, want %q", mc.DefaultImage, "alpine:3.19")
	}
}

func TestWithVercelConfig(t *testing.T) {
	cfg := DefaultConfig()
	vercelCfg := VercelConfig{
		Token:   "test-token",
		TeamID:  "team-123",
		Runtime: "node22",
	}
	WithVercelConfig(vercelCfg)(cfg)

	if cfg.Provider != "vercel" {
		t.Errorf("Provider = %q, want %q", cfg.Provider, "vercel")
	}
	vc, ok := cfg.ProviderConfig.(VercelConfig)
	if !ok {
		t.Fatal("ProviderConfig should be VercelConfig")
	}
	if vc.Token != "test-token" {
		t.Errorf("VercelConfig.Token = %q, want %q", vc.Token, "test-token")
	}
}

func TestWithE2BConfig(t *testing.T) {
	cfg := DefaultConfig()
	e2bCfg := E2BConfig{
		APIKey:   "test-key",
		Template: "python",
		Timeout:  5 * time.Minute,
	}
	WithE2BConfig(e2bCfg)(cfg)

	if cfg.Provider != "e2b" {
		t.Errorf("Provider = %q, want %q", cfg.Provider, "e2b")
	}
	ec, ok := cfg.ProviderConfig.(E2BConfig)
	if !ok {
		t.Fatal("ProviderConfig should be E2BConfig")
	}
	if ec.APIKey != "test-key" {
		t.Errorf("E2BConfig.APIKey = %q, want %q", ec.APIKey, "test-key")
	}
}

func TestWithKubernetesConfig(t *testing.T) {
	cfg := DefaultConfig()
	k8sCfg := KubernetesConfig{
		Namespace: "sandbox",
		Image:     "python:3.11",
	}
	WithKubernetesConfig(k8sCfg)(cfg)

	if cfg.Provider != "kubernetes" {
		t.Errorf("Provider = %q, want %q", cfg.Provider, "kubernetes")
	}
}

func TestWithPodmanConfig(t *testing.T) {
	cfg := DefaultConfig()
	podmanCfg := PodmanConfig{
		URI: "unix:///run/podman/podman.sock",
	}
	WithPodmanConfig(podmanCfg)(cfg)

	if cfg.Provider != "podman" {
		t.Errorf("Provider = %q, want %q", cfg.Provider, "podman")
	}
}

func TestWithFirecrackerConfig(t *testing.T) {
	cfg := DefaultConfig()
	fcCfg := FirecrackerConfig{
		VCPUCount:  2,
		MemSizeMiB: 1024,
	}
	WithFirecrackerConfig(fcCfg)(cfg)

	if cfg.Provider != "firecracker" {
		t.Errorf("Provider = %q, want %q", cfg.Provider, "firecracker")
	}
}

func TestResourceConfigToProviderConfig(t *testing.T) {
	rc := ResourceConfig{
		MemoryMB: 512,
		CPUs:     2,
		DiskMB:   1024,
	}
	pc := rc.ToProviderConfig()

	if pc.MemoryMB != 512 {
		t.Errorf("MemoryMB = %d, want 512", pc.MemoryMB)
	}
	if pc.CPUs != 2 {
		t.Errorf("CPUs = %f, want 2", pc.CPUs)
	}
	if pc.DiskMB != 1024 {
		t.Errorf("DiskMB = %d, want 1024", pc.DiskMB)
	}
}

func TestDefaultExecuteConfig(t *testing.T) {
	cfg := DefaultExecuteConfig()

	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want %v", cfg.Timeout, 30*time.Second)
	}
	if cfg.WorkDir != "/workspace" {
		t.Errorf("WorkDir = %q, want %q", cfg.WorkDir, "/workspace")
	}
	if cfg.Env == nil {
		t.Error("Env should not be nil")
	}
	if cfg.Files == nil {
		t.Error("Files should not be nil")
	}
}

func TestExecuteOptions(t *testing.T) {
	t.Run("WithLanguage", func(t *testing.T) {
		cfg := DefaultExecuteConfig()
		WithLanguage("Go")(cfg)
		if cfg.Language != "Go" {
			t.Errorf("Language = %q, want %q", cfg.Language, "Go")
		}
	})

	t.Run("WithFilename", func(t *testing.T) {
		cfg := DefaultExecuteConfig()
		WithFilename("main.py")(cfg)
		if cfg.Filename != "main.py" {
			t.Errorf("Filename = %q, want %q", cfg.Filename, "main.py")
		}
	})

	t.Run("WithExecutionTimeout", func(t *testing.T) {
		cfg := DefaultExecuteConfig()
		WithExecutionTimeout(1 * time.Minute)(cfg)
		if cfg.Timeout != 1*time.Minute {
			t.Errorf("Timeout = %v, want %v", cfg.Timeout, 1*time.Minute)
		}
	})

	t.Run("WithEnv", func(t *testing.T) {
		cfg := DefaultExecuteConfig()
		env := map[string]string{"FOO": "bar"}
		WithEnv(env)(cfg)
		if cfg.Env["FOO"] != "bar" {
			t.Errorf("Env[FOO] = %q, want %q", cfg.Env["FOO"], "bar")
		}
	})

	t.Run("WithStdin", func(t *testing.T) {
		cfg := DefaultExecuteConfig()
		WithStdin("input data")(cfg)
		if cfg.Stdin != "input data" {
			t.Errorf("Stdin = %q, want %q", cfg.Stdin, "input data")
		}
	})

	t.Run("WithFiles", func(t *testing.T) {
		cfg := DefaultExecuteConfig()
		files := map[string][]byte{"test.txt": []byte("content")}
		WithFiles(files)(cfg)
		if string(cfg.Files["test.txt"]) != "content" {
			t.Errorf("Files[test.txt] = %q, want %q", cfg.Files["test.txt"], "content")
		}
	})

	t.Run("WithWorkDir", func(t *testing.T) {
		cfg := DefaultExecuteConfig()
		WithWorkDir("/app")(cfg)
		if cfg.WorkDir != "/app" {
			t.Errorf("WorkDir = %q, want %q", cfg.WorkDir, "/app")
		}
	})

	t.Run("WithKeepArtifacts", func(t *testing.T) {
		cfg := DefaultExecuteConfig()
		WithKeepArtifacts()(cfg)
		if !cfg.KeepArtifacts {
			t.Error("KeepArtifacts should be true")
		}
	})
}

func TestNopLogger(t *testing.T) {
	logger := NopLogger{}

	// Should not panic
	logger.Debug("test")
	logger.Info("test")
	logger.Warn("test")
	logger.Error("test")
}
