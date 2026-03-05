//go:build linux

package sindoq

import (
	_ "github.com/happyhackingspace/sindoq/internal/provider/firecracker"
	_ "github.com/happyhackingspace/sindoq/internal/provider/gvisor"
	_ "github.com/happyhackingspace/sindoq/internal/provider/landlock"
	_ "github.com/happyhackingspace/sindoq/internal/provider/nsjail"
)
