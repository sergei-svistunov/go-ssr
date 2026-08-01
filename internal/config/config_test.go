package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRead_WebDirIsRelativeToConfigFile(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "webDir: ./internal/web\nwebPackage: example.com/app/internal/web\n")

	sub := filepath.Join(root, "internal", "web", "pages")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	// Reading from a subdirectory must resolve webDir against the config file,
	// not against the working directory.
	t.Chdir(sub)

	cfg, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	want := filepath.Join(root, "internal", "web")
	if cfg.WebDir != want {
		t.Errorf("WebDir = %q, want %q", cfg.WebDir, want)
	}
	if cfg.Dir != root {
		t.Errorf("Dir = %q, want %q", cfg.Dir, root)
	}
}

func TestRead_AbsoluteWebDirIsKept(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(t.TempDir(), "elsewhere")
	writeConfig(t, root, "webDir: "+abs+"\nwebPackage: example.com/app/internal/web\n")

	t.Chdir(root)

	cfg, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if cfg.WebDir != abs {
		t.Errorf("WebDir = %q, want %q", cfg.WebDir, abs)
	}
}

func TestRead_DepsTypeDefaults(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "webPackage: example.com/app/internal/web\ndepsPackage: example.com/app/internal/model\n")

	t.Chdir(root)

	cfg, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if cfg.DepsType != "Deps" {
		t.Errorf("DepsType = %q, want %q", cfg.DepsType, "Deps")
	}
}

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
