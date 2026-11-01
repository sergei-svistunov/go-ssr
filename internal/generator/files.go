package generator

import (
	"os"
	"path/filepath"
)

// writeFile writes data to name, a path inside dir, with mode 0644, creating
// missing folders with mode 0755. It never follows a symbolic link out of dir.
func writeFile(dir, name string, data []byte) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	return root.WriteFile(name, data, 0o644)
}
