package generator

import (
	"fmt"
	"os/exec"
)

// runCommand runs an external tool with the generator's configured streams.
// Subprocesses inherit whatever stdin the caller configured, which is nothing
// unless it opted in — a subprocess reading a protocol stream would break it.
func (g *Generator) runCommand(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdin = g.in
	cmd.Stdout = g.out
	cmd.Stderr = g.errOut

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v: %w", name, args, err)
	}
	return nil
}

// GoModTidy resolves the module requirements of a freshly scaffolded project.
func (g *Generator) GoModTidy() error {
	return g.runCommand(g.dir, "go", "mod", "tidy")
}
