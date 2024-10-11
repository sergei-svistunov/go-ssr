package generator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func (g *Generator) Webpack() error {
	needUpdateNpmModules, err := g.needUpdateNpmModules()
	if err != nil {
		return err
	}
	if needUpdateNpmModules {
		if err := g.installNpmModules(); err != nil {
			return err
		}
	}

	mode := "development"
	if g.prod {
		mode = "production"
	}
	cmd := exec.Command("npx", "webpack", "--mode", mode)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = g.webDir

	_, _ = fmt.Fprintln(os.Stderr, "Building static...")
	return cmd.Run()
}

func (g *Generator) installNpmModules() error {
	cmd := exec.Command("npm", "install")
	cmd.Dir = g.webDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func (g *Generator) needUpdateNpmModules() (bool, error) {
	packageLock, err := fileTime(filepath.Join(g.webDir, "package-lock.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}

	packageJson, err := fileTime(filepath.Join(g.webDir, "package.json"))
	if err != nil {
		return false, err
	}

	return packageLock.Before(packageJson), nil
}
