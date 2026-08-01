package boilerplate

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/sergei-svistunov/go-ssr/internal/config"
)

//go:embed files
var files embed.FS

// DefaultWebDir is where a scaffolded project keeps its templates and assets.
const DefaultWebDir = "internal/web"

// Options describes the project to scaffold.
type Options struct {
	Dir         string // target directory; must exist and be empty
	PkgName     string // Go module path
	WebDir      string // defaults to DefaultWebDir; must stay inside the project
	DepsPackage string // optional package injected into every data provider; must be inside PkgName
	DepsType    string // type inside DepsPackage; defaults to Deps when DepsPackage is set
}

// normalize fills in the defaults and rejects options that would scaffold a
// project that cannot build or that writes outside its own directory.
func (o *Options) normalize() error {
	if o.Dir == "" {
		o.Dir = "."
	}
	if o.PkgName == "" {
		return errors.New("module path is required")
	}

	if o.WebDir == "" {
		o.WebDir = DefaultWebDir
	}
	o.WebDir = path.Clean(filepath.ToSlash(o.WebDir))
	if path.IsAbs(o.WebDir) || filepath.IsAbs(filepath.FromSlash(o.WebDir)) ||
		o.WebDir == "." || o.WebDir == ".." || strings.HasPrefix(o.WebDir, "../") {
		return fmt.Errorf("webDir %q must be a relative path inside the project, such as %q", o.WebDir, DefaultWebDir)
	}

	if o.DepsPackage != "" {
		if !strings.HasPrefix(o.DepsPackage, o.PkgName+"/") {
			return fmt.Errorf("depsPackage %q must be inside module %q so it can be created together with the project", o.DepsPackage, o.PkgName)
		}
		if rel := strings.TrimPrefix(o.DepsPackage, o.PkgName+"/"); rel != path.Clean(rel) {
			return fmt.Errorf("depsPackage %q is not a clean import path", o.DepsPackage)
		}
		if o.DepsType == "" {
			o.DepsType = "Deps"
		}
		if !token.IsIdentifier(path.Base(o.DepsPackage)) {
			return fmt.Errorf("the last element of depsPackage %q is not a valid Go package name", o.DepsPackage)
		}
		if !token.IsIdentifier(o.DepsType) {
			return fmt.Errorf("depsType %q is not a valid Go type name", o.DepsType)
		}
	}

	return nil
}

// Init scaffolds a new project.
func Init(opts Options) error {
	if err := opts.normalize(); err != nil {
		return err
	}

	entries, err := os.ReadDir(opts.Dir)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("directory is not empty")
	}

	cfg := config.Config{
		WebDir:      "./" + opts.WebDir,
		WebPackage:  path.Join(opts.PkgName, opts.WebDir),
		DepsPackage: opts.DepsPackage,
		DepsType:    opts.DepsType,
	}
	if err := config.InitAt(opts.Dir, cfg); err != nil {
		return err
	}

	if err := writeGoMod(opts.Dir, opts.PkgName); err != nil {
		return err
	}

	if err := recursiveCopy("files/web", filepath.Join(opts.Dir, filepath.FromSlash(opts.WebDir)), opts); err != nil {
		return err
	}

	// Without an injected dependency the plain templates already compile; with
	// one, the entry point, the handler wiring and the dependency package itself
	// all have to agree on the type, so they are written from their own set.
	if opts.DepsPackage == "" {
		return copyGoFile("files/main.go", filepath.Join(opts.Dir, "main.go"), opts)
	}

	depsDir := filepath.Join(opts.Dir, filepath.FromSlash(strings.TrimPrefix(opts.DepsPackage, opts.PkgName+"/")))
	if err := os.MkdirAll(depsDir, 0755); err != nil {
		return err
	}
	for src, dest := range map[string]string{
		"files/deps/deps.go.tmpl": filepath.Join(depsDir, "deps.go"),
		"files/deps/main.go.tmpl": filepath.Join(opts.Dir, "main.go"),
		"files/deps/web.go.tmpl":  filepath.Join(opts.Dir, filepath.FromSlash(opts.WebDir), "web.go"),
	} {
		if err := copyGoFile(src, dest, opts); err != nil {
			return err
		}
	}

	return nil
}

// Files returns the paths, relative to the project directory, that Init writes.
// A caller can report exactly what was created without walking the tree.
func Files(opts Options) ([]string, error) {
	if err := opts.normalize(); err != nil {
		return nil, err
	}

	out := []string{config.FileName, "go.mod", "main.go"}
	if opts.DepsPackage != "" {
		out = append(out, path.Join(strings.TrimPrefix(opts.DepsPackage, opts.PkgName+"/"), "deps.go"))
	}

	err := fs.WalkDir(files, "files/web", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel("files/web", p)
		if err != nil {
			return err
		}
		out = append(out, path.Join(opts.WebDir, filepath.ToSlash(rel)))
		return nil
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}

func recursiveCopy(srcDir, destDir string, opts Options) error {
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
			return copyGoFile(path, destPath, opts)
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

func copyGoFile(templateFile, outFile string, opts Options) error {
	content, err := files.ReadFile(templateFile)
	if err != nil {
		return err
	}

	depsPkg := ""
	if opts.DepsPackage != "" {
		depsPkg = path.Base(opts.DepsPackage)
	}

	// The web directory is configurable, so the templates cannot hardcode the
	// import path that points at it.
	for placeholder, value := range map[string]string{
		"<PKG_NAME>":     opts.PkgName,
		"<WEB_DIR>":      opts.WebDir,
		"<DEPS_PACKAGE>": opts.DepsPackage,
		"<DEPS_PKG>":     depsPkg,
		"<DEPS_TYPE>":    opts.DepsType,
	} {
		content = bytes.ReplaceAll(content, []byte(placeholder), []byte(value))
	}

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

func writeGoMod(dir, pkgName string) error {
	f, err := os.Create(filepath.Join(dir, "go.mod"))
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString("module " + pkgName + "\n\ngo 1.25\n"); err != nil {
		return err
	}
	return nil
}
