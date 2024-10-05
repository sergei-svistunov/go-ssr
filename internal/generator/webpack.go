package generator

import (
	"fmt"
	"os"
	"os/exec"
)

func (g *Generator) Webpack() error {
	cmd := exec.Command("npx", "webpack", "--mode", "development")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = g.webDir

	_, _ = fmt.Fprintln(os.Stderr, "Building static...")
	return cmd.Run()
}
