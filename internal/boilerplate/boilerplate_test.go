package boilerplate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergei-svistunov/go-ssr/internal/boilerplate"
)

// The web directory is configurable, so the scaffolded entry point and handler
// wiring must import it where it actually is.
func TestInit_CustomWebDirIsImportedCorrectly(t *testing.T) {
	dir := t.TempDir()

	opts := boilerplate.Options{Dir: dir, PkgName: "example.com/app", WebDir: "web"}
	if err := boilerplate.Init(opts); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if got := read(t, filepath.Join(dir, "main.go")); !strings.Contains(got, `"example.com/app/web"`) {
		t.Errorf("main.go does not import the configured web directory:\n%s", got)
	}
	if got := read(t, filepath.Join(dir, "web", "web.go")); !strings.Contains(got, `"example.com/app/web/pages"`) {
		t.Errorf("web.go does not import the configured pages directory:\n%s", got)
	}
	if got := read(t, filepath.Join(dir, "gossr.yaml")); !strings.Contains(got, "webDir: ./web") {
		t.Errorf("gossr.yaml does not record the configured web directory:\n%s", got)
	}

	files, err := boilerplate.Files(opts)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
			t.Errorf("Files reported %s, which was not written: %v", f, err)
		}
	}
}

// A web directory outside the project would write over files the emptiness
// check never looked at.
func TestInit_RejectsAWebDirOutsideTheProject(t *testing.T) {
	for _, webDir := range []string{"../shared", "/tmp/shared", ".."} {
		dir := t.TempDir()
		err := boilerplate.Init(boilerplate.Options{Dir: dir, PkgName: "example.com/app", WebDir: webDir})
		if err == nil {
			t.Errorf("webDir %q was accepted", webDir)
			continue
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("webDir %q wrote %d entries before failing", webDir, len(entries))
		}
	}
}

// An injected dependency changes three files at once: the package itself, the
// handler wiring that takes it, and the entry point that constructs it.
func TestInit_DepsPackageIsScaffolded(t *testing.T) {
	dir := t.TempDir()

	opts := boilerplate.Options{
		Dir:         dir,
		PkgName:     "example.com/app",
		DepsPackage: "example.com/app/internal/model",
		DepsType:    "Model",
	}
	if err := boilerplate.Init(opts); err != nil {
		t.Fatalf("Init: %v", err)
	}

	deps := read(t, filepath.Join(dir, "internal", "model", "deps.go"))
	if !strings.Contains(deps, "package model") || !strings.Contains(deps, "type Model struct") {
		t.Errorf("the deps package was not scaffolded for the configured type:\n%s", deps)
	}

	web := read(t, filepath.Join(dir, "internal", "web", "web.go"))
	if !strings.Contains(web, "func New(d *model.Model)") || !strings.Contains(web, "NewSsrHandler(d,") {
		t.Errorf("web.go does not pass the injected value to the handler:\n%s", web)
	}

	main := read(t, filepath.Join(dir, "main.go"))
	if !strings.Contains(main, "web.New(model.New())") {
		t.Errorf("main.go does not construct the injected value:\n%s", main)
	}
}

func TestInit_RejectsADepsPackageOutsideTheModule(t *testing.T) {
	dir := t.TempDir()
	err := boilerplate.Init(boilerplate.Options{
		Dir: dir, PkgName: "example.com/app", DepsPackage: "example.com/other/model",
	})
	if err == nil {
		t.Fatal("a deps package outside the module was accepted, but nothing can create it")
	}
	if !strings.Contains(err.Error(), "example.com/app") {
		t.Errorf("the error should name the module: %v", err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	return string(b)
}
