package generator

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
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

	_, _ = fmt.Fprintln(g.errOut, "Building static...")
	return g.runCommand(g.webDir, "npx", "webpack", "--mode", mode)
}

func (g *Generator) installNpmModules() error {
	return g.runCommand(g.webDir, "npm", "install")
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

// assetSourceExtensions are the file types webpack turns into hashed output.
// A change to any of them means the previous build no longer describes the app.
var assetSourceExtensions = map[string]bool{
	".ts": true, ".tsx": true, ".js": true, ".mjs": true,
	".scss": true, ".sass": true, ".css": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".svg": true, ".webp": true, ".ico": true, ".avif": true,
}

// AssetsStale reports whether the webpack build output is older than the sources
// it was built from, along with a short reason suitable for showing to a caller.
// It lets a caller build assets only when it would change something, instead of
// paying for an npm and webpack run on every regeneration.
func (g *Generator) AssetsStale() (bool, string, error) {
	// Nothing on disk records which mode produced the current bundles, and
	// serving development bundles from a release binary is worse than an extra
	// build, so a production run always rebuilds.
	if g.prod {
		return true, "a production build was requested and the existing bundles may be development ones", nil
	}

	assetsTime, err := fileTime(filepath.Join(g.webDir, "webpack-assets.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return true, "webpack-assets.json is missing — assets have never been built", nil
		}
		return false, "", err
	}

	needNpm, err := g.needUpdateNpmModules()
	if err != nil {
		return false, "", err
	}
	if needNpm {
		// needUpdateNpmModules also reports true when the lock file is absent, so
		// the reason has to cover both cases rather than claim a comparison that
		// was never made.
		return true, "npm dependencies need installing (package-lock.json is missing or older than package.json)", nil
	}

	// Build configuration counts as a source: changing it changes the output.
	for _, name := range []string{"webpack.config.js", "webpack.config.mjs", "webpack.config.ts", "tsconfig.json"} {
		p := filepath.Join(g.webDir, name)
		t, err := fileTime(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, "", err
		}
		if t.After(assetsTime) {
			return true, fmt.Sprintf("%s is newer than the last build", g.relPath(p)), nil
		}
	}

	// Sources live anywhere under the web directory, not only under pages/: a
	// route's index.ts or styles.scss routinely imports shared code from a
	// sibling directory, and an edit there changes the bundles just as much.
	skipDirs := map[string]bool{}
	if a, err := AssetsFromDir(g.webDir); err == nil && a.outputPath != "" {
		// The webpack output directory holds the build, not its sources, and its
		// files are always at least as new as the manifest that describes them.
		skipDirs[filepath.Clean(filepath.Join(g.webDir, a.outputPath))] = true
	}

	newer, err := newerSourceThan(g.webDir, assetsTime, skipDirs)
	if err != nil {
		return false, "", err
	}
	if newer != "" {
		return true, fmt.Sprintf("%s is newer than the last build", g.relPath(newer)), nil
	}

	return false, "", nil
}

// newerSourceThan returns the first asset source under dir modified after t, or
// an empty string when the build output is up to date. Directories in skipDirs,
// along with dependency and build-output directories, are not sources.
func newerSourceThan(dir string, t time.Time, skipDirs map[string]bool) (string, error) {
	var found string

	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			// The staging directory holds build output, not sources.
			if d.Name() == staticEmbedDirName || d.Name() == "node_modules" || skipDirs[filepath.Clean(p)] {
				return filepath.SkipDir
			}
			return nil
		}
		if !assetSourceExtensions[filepath.Ext(p)] {
			return nil
		}
		// The generator rewrites its own TypeScript on every run, after this
		// check has already read the timestamps. Counting it as a source would
		// make the assets permanently stale and force a webpack run every time.
		if d.Name() == tsTypesFileName {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(t) {
			found = p
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	return found, nil
}
