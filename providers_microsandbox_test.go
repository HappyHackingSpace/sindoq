//go:build microsandbox && cgo

package sindoq

import (
	"reflect"
	"testing"

	"github.com/happyhackingspace/sindoq/internal/factory"
)

func TestMicrosandboxConfigReachesProvider(t *testing.T) {
	constructor, ok := factory.DefaultRegistry.GetConstructor("microsandbox")
	if !ok {
		t.Fatal("microsandbox provider should be registered")
	}
	p, err := constructor(MicrosandboxConfig{DefaultImage: "alpine:3.19", SkipRuntimeInstall: true})
	if err != nil {
		t.Fatalf("public MicrosandboxConfig should be accepted, got %v", err)
	}
	// The provider keeps its config unexported; read it to check the fields were forwarded.
	cfg := reflect.ValueOf(p).Elem().FieldByName("config").Elem()
	if got := cfg.FieldByName("DefaultImage").String(); got != "alpine:3.19" {
		t.Errorf("DefaultImage = %q, want %q", got, "alpine:3.19")
	}
	if !cfg.FieldByName("SkipRuntimeInstall").Bool() {
		t.Error("SkipRuntimeInstall was not forwarded")
	}
	if _, err := constructor(nil); err != nil {
		t.Errorf("nil config should use defaults, got %v", err)
	}
	if _, err := constructor(DockerConfig{}); err == nil {
		t.Error("unrelated config type should be rejected")
	}
}
