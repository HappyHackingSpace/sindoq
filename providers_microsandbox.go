//go:build microsandbox && cgo

package sindoq

import (
	"github.com/happyhackingspace/sindoq/internal/factory"
	"github.com/happyhackingspace/sindoq/internal/provider"
	"github.com/happyhackingspace/sindoq/internal/provider/microsandbox"
)

// The provider package cannot import sindoq (import cycle), so the public
// MicrosandboxConfig is converted here before reaching its constructor.
func init() {
	next, ok := factory.DefaultRegistry.GetConstructor("microsandbox")
	if !ok {
		return
	}
	factory.Register("microsandbox", func(config any) (provider.Provider, error) {
		if cfg, ok := config.(MicrosandboxConfig); ok {
			config = &microsandbox.Config{
				DefaultImage:       cfg.DefaultImage,
				SkipRuntimeInstall: cfg.SkipRuntimeInstall,
			}
		}
		return next(config)
	})
}
