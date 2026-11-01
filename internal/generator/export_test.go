package generator

import (
	"context"
	"os/exec"
	"sync"
)

// SetGoEnvCommand makes the generator run cmd to find GOROOT, forgetting what
// it found before, until restore runs.
func SetGoEnvCommand(cmd func(ctx context.Context) *exec.Cmd) (restore func()) {
	saved := goEnvCommand
	goEnvCommand = cmd
	goroot.once, goroot.dir, goroot.err = sync.Once{}, "", nil
	return func() {
		goEnvCommand = saved
		goroot.once, goroot.dir, goroot.err = sync.Once{}, "", nil
	}
}
