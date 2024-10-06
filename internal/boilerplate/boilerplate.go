package boilerplate

import (
	"bytes"
	"embed"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"github.com/sergei-svistunov/go-ssr/internal/config"
)

//go:embed files
var files embed.FS

func Init(pkgName, webDir string) error {
	entries, err := os.ReadDir(".")
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("directory is not empty")
	}

	webPkgName := path.Join(pkgName, webDir)
	if err := config.Init(webPkgName); err != nil {
		return err
	}

	if err := writeGoMod(pkgName); err != nil {
		return err
	}

	if err := copyGoFile("files/main.go", "main.go", pkgName); err != nil {
		return err
	}

	if err := recursiveCopy("files/web", webDir, pkgName); err != nil {
		return err
	}

	if err := installNpmModules(webDir); err != nil {
		return err
	}

	return nil
}

func recursiveCopy(srcDir, destDir, pkgName string) error {
	return fs.WalkDir(files, srcDir, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(destDir, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		if filepath.Ext(destPath) == ".go" {
			return copyGoFile(path, destPath, pkgName)
		}
		return copyFile(path, destPath)
	})
}

func copyFile(srcFile, destFile string) error {
	src, err := files.Open(srcFile)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(destFile)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	if err != nil {
		return err
	}

	return nil
}

func copyGoFile(templateFile, outFile, pkgName string) error {
	content, err := files.ReadFile(templateFile)
	if err != nil {
		return err
	}

	content = bytes.Replace(content, []byte("<PKG_NAME>"), []byte(pkgName), 1)

	f, err := os.Create(outFile)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(content); err != nil {
		return err
	}
	return nil
}

func writeGoMod(pkgName string) error {
	f, err := os.Create("go.mod")
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString("module " + pkgName + "\n\ngo 1.22\n"); err != nil {
		return err
	}
	return nil
}

func installNpmModules(webDir string) error {
	cmd := exec.Command("npm", "install")
	cmd.Dir = webDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
