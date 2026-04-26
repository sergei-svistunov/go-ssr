package generator

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergei-svistunov/go-ssr/internal/generator/route"
)

func setupStaticFixture(t *testing.T, files map[string]string) *Generator {
	t.Helper()

	webDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(webDir, "pages"), 0755); err != nil {
		t.Fatal(err)
	}
	distDir := filepath.Join(webDir, "dist")
	if err := os.MkdirAll(distDir, 0755); err != nil {
		t.Fatal(err)
	}

	for name, contents := range files {
		full := filepath.Join(distDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}

	return &Generator{
		webDir: webDir,
		assets: &Assets{outputPath: "dist"},
	}
}

func TestGenStaticFiles_NoSrcEmitsNothing(t *testing.T) {
	webDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(webDir, "pages"), 0755); err != nil {
		t.Fatal(err)
	}
	g := &Generator{webDir: webDir, assets: &Assets{outputPath: "dist"}}

	hasStatic, _, err := g.genStaticFiles()
	if err != nil {
		t.Fatal(err)
	}
	if hasStatic {
		t.Fatal("expected hasStatic=false when no src dir")
	}
	if _, err := os.Stat(filepath.Join(webDir, "pages", staticGenFileName)); !os.IsNotExist(err) {
		t.Fatalf("expected no generated file when no src; stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(webDir, "pages", staticEmbedDirName)); !os.IsNotExist(err) {
		t.Fatalf("expected no embed dir when no src; stat err=%v", err)
	}
}

func TestGenStaticFiles_EmbedsAndCompresses(t *testing.T) {
	cssBody := strings.Repeat("body{color:red;}\n", 50)
	g := setupStaticFixture(t, map[string]string{
		"app.css":   cssBody,
		"logo.png":  "fakepngbytes",
		"sub/x.js":  strings.Repeat("var a=1;\n", 100),
		"icon.woff": "ffff",
	})

	hasStatic, _, err := g.genStaticFiles()
	if err != nil {
		t.Fatal(err)
	}
	if !hasStatic {
		t.Fatal("expected hasStatic=true")
	}

	embDir := filepath.Join(g.webDir, "pages", staticEmbedDirName)
	for _, p := range []string{"app.css.gz", "logo.png", "sub/x.js.gz", "icon.woff"} {
		if _, err := os.Stat(filepath.Join(embDir, filepath.FromSlash(p))); err != nil {
			t.Fatalf("expected staged file %s: %v", p, err)
		}
	}
	// PNG and woff must NOT be re-compressed.
	if _, err := os.Stat(filepath.Join(embDir, "logo.png.gz")); !os.IsNotExist(err) {
		t.Fatalf("png should not be gzipped")
	}
	if _, err := os.Stat(filepath.Join(embDir, "icon.woff.gz")); !os.IsNotExist(err) {
		t.Fatalf("woff should not be gzipped")
	}

	// Verify gzipped content roundtrips.
	gz, err := os.ReadFile(filepath.Join(embDir, "app.css.gz"))
	if err != nil {
		t.Fatal(err)
	}
	gzr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}
	got, err := readAllAndClose(gzr)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != cssBody {
		t.Fatalf("gzipped css did not roundtrip")
	}

	// Manifest file must exist with all 4 entries.
	manifest := loadManifest(filepath.Join(embDir, ".etags.json"))
	if len(manifest) != 4 {
		t.Fatalf("manifest entries = %d, want 4", len(manifest))
	}

	// Generated Go file must exist and contain the expected URL keys + sizes.
	out, err := os.ReadFile(filepath.Join(g.webDir, "pages", staticGenFileName))
	if err != nil {
		t.Fatal(err)
	}
	src := string(out)
	for _, want := range []string{
		`"/dist/app.css"`,
		`"/dist/logo.png"`,
		`"/dist/sub/x.js"`,
		`"/dist/icon.woff"`,
		`var ssrStaticFS embed.FS`,
		`func ssrServeStatic(`,
		`static.Serve(`,
		`map[string]static.File{`,
		// Must use the all: prefix so go:embed includes underscore-prefixed
		// dirs like _userId_/ that come from dynamic-param routes.
		`//go:embed all:` + staticEmbedDirName,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("generated file missing %q", want)
		}
	}
}

func TestGenStaticFiles_UnderscorePrefixedPathStaged(t *testing.T) {
	g := setupStaticFixture(t, map[string]string{
		"js/pages/users/_userId_/info.abc.js": "var x = 1;",
	})
	if _, _, err := g.genStaticFiles(); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(g.webDir, "pages", staticEmbedDirName, "js", "pages", "users", "_userId_", "info.abc.js.gz")
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("expected underscore-prefixed path to be staged: %v", err)
	}
	out, err := os.ReadFile(filepath.Join(g.webDir, "pages", staticGenFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "//go:embed all:"+staticEmbedDirName) {
		t.Fatal("generated file must use 'all:' embed prefix")
	}
	if !strings.Contains(string(out), `"/dist/js/pages/users/_userId_/info.abc.js"`) {
		t.Fatal("URL key for _userId_ asset missing from generated map")
	}
}

func TestGenStaticFiles_PrunesEmptyDirs(t *testing.T) {
	g := setupStaticFixture(t, map[string]string{
		"deep/sub/dir/a.css": "x",
	})
	if _, _, err := g.genStaticFiles(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(g.webDir, "dist", "deep", "sub", "dir", "a.css")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.genStaticFiles(); err != nil {
		t.Fatal(err)
	}
	stagedDir := filepath.Join(g.webDir, "pages", staticEmbedDirName, "deep")
	if _, err := os.Stat(stagedDir); !os.IsNotExist(err) {
		t.Fatalf("expected empty staged dir tree to be pruned, stat err=%v", err)
	}
}

func TestGenStaticFiles_OutputPathWithSlashes(t *testing.T) {
	webDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(webDir, "pages"), 0755); err != nil {
		t.Fatal(err)
	}
	distDir := filepath.Join(webDir, "dist")
	if err := os.MkdirAll(distDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "a.css"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// Misconfigured outputPath with surrounding slashes — must produce clean URL keys.
	g := &Generator{webDir: webDir, assets: &Assets{outputPath: "/dist/"}}
	if _, _, err := g.genStaticFiles(); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(filepath.Join(webDir, "pages", staticGenFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"/dist/a.css"`) {
		t.Fatalf("expected /dist/a.css URL key, got:\n%s", out)
	}
	if strings.Contains(string(out), `"//dist/`) {
		t.Fatalf("URL key has double slash, got:\n%s", out)
	}
}

func TestGenStaticFiles_CacheHitReusesEntry(t *testing.T) {
	g := setupStaticFixture(t, map[string]string{
		"a.css": "body{color:red}",
	})
	if _, _, err := g.genStaticFiles(); err != nil {
		t.Fatal(err)
	}

	gzPath := filepath.Join(g.webDir, "pages", staticEmbedDirName, "a.css.gz")
	info1, err := os.Stat(gzPath)
	if err != nil {
		t.Fatal(err)
	}

	// Re-run without changing the source. Cache should hit and the staged file
	// should be untouched.
	if _, _, err := g.genStaticFiles(); err != nil {
		t.Fatal(err)
	}
	info2, err := os.Stat(gzPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info2.ModTime().Equal(info1.ModTime()) {
		t.Fatalf("cache hit re-wrote staged file: mtime %v -> %v", info1.ModTime(), info2.ModTime())
	}

	// Change source content — cache should miss and re-stage with new bytes.
	srcPath := filepath.Join(g.webDir, "dist", "a.css")
	if err := os.WriteFile(srcPath, []byte("body{color:blue}"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.genStaticFiles(); err != nil {
		t.Fatal(err)
	}
	gzBytes, err := os.ReadFile(gzPath)
	if err != nil {
		t.Fatal(err)
	}
	gzr, err := gzip.NewReader(bytes.NewReader(gzBytes))
	if err != nil {
		t.Fatalf("staged .gz invalid: %v", err)
	}
	got, err := readAllAndClose(gzr)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "body{color:blue}" {
		t.Fatalf("cache did not re-stage on content change: got %q", got)
	}
}

// TestGenStaticFiles_ContentChangedSameMtime guards against the cache hazard
// where tools like git checkout, rsync -a, tar -p preserve mtimes — a
// content change with an unchanged mtime must still bust the cache.
func TestGenStaticFiles_ContentChangedSameMtime(t *testing.T) {
	g := setupStaticFixture(t, map[string]string{
		"a.css": "version-one-content",
	})
	srcPath := filepath.Join(g.webDir, "dist", "a.css")
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	frozenMtime := srcInfo.ModTime()

	if _, _, err := g.genStaticFiles(); err != nil {
		t.Fatal(err)
	}

	// Overwrite content but force mtime back to its original value, simulating
	// what `tar -p` / `git checkout` / `rsync -a` do with stored timestamps.
	if err := os.WriteFile(srcPath, []byte("version-two-different-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(srcPath, frozenMtime, frozenMtime); err != nil {
		t.Fatal(err)
	}

	if _, _, err := g.genStaticFiles(); err != nil {
		t.Fatal(err)
	}

	gzPath := filepath.Join(g.webDir, "pages", staticEmbedDirName, "a.css.gz")
	gzBytes, err := os.ReadFile(gzPath)
	if err != nil {
		t.Fatal(err)
	}
	gzr, err := gzip.NewReader(bytes.NewReader(gzBytes))
	if err != nil {
		t.Fatalf("staged .gz invalid: %v", err)
	}
	got, err := readAllAndClose(gzr)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "version-two-different-bytes" {
		t.Fatalf("staged content not refreshed: got %q, want v2", got)
	}
}

func TestGenStaticFiles_PrunesRemovedFiles(t *testing.T) {
	g := setupStaticFixture(t, map[string]string{
		"keep.css": "x",
		"drop.css": "y",
	})
	if _, _, err := g.genStaticFiles(); err != nil {
		t.Fatal(err)
	}

	dropStaged := filepath.Join(g.webDir, "pages", staticEmbedDirName, "drop.css.gz")
	if _, err := os.Stat(dropStaged); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(g.webDir, "dist", "drop.css")); err != nil {
		t.Fatal(err)
	}

	if _, _, err := g.genStaticFiles(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dropStaged); !os.IsNotExist(err) {
		t.Fatalf("expected pruned %s, stat err=%v", dropStaged, err)
	}

	// Manifest should no longer contain the dropped entry.
	manifestRaw, err := os.ReadFile(filepath.Join(g.webDir, "pages", staticEmbedDirName, ".etags.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]manifestEntry
	if err := json.Unmarshal(manifestRaw, &m); err != nil {
		t.Fatal(err)
	}
	if _, exists := m["drop.css.gz"]; exists {
		t.Fatalf("manifest still contains pruned entry")
	}
	if _, exists := m["keep.css.gz"]; !exists {
		t.Fatalf("manifest missing kept entry")
	}
}

func TestCollisionsWithRoutes(t *testing.T) {
	g := &Generator{routes: map[string]*route.Route{
		"/":                 nil,
		"/home":             nil,
		"/dist/foo":         nil,
		"/users/_id_":       nil,
		"/users/_id_/info":  nil,
		"/api/_v_/_action_": nil,
	}}

	got := g.collisionsWithRoutes([]string{
		"/dist/app.css",      // no route at /dist/*  → no collision
		"/dist/foo",          // exact match → collision
		"/home",              // exact match → collision
		"/users/abc",         // matches /users/_id_ → collision
		"/users/abc/info",    // matches /users/_id_/info → collision
		"/users/abc/x.css",   // depth 3, no matching route → no collision
		"/api/v1/list",       // matches /api/_v_/_action_ → collision
		"/dist/users/_id_",   // /_id_/ in static URL is literal text, not a route → no collision (no /dist/users/_id_ route)
	})
	want := []string{"/api/v1/list", "/dist/foo", "/home", "/users/abc", "/users/abc/info"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	if c := g.collisionsWithRoutes([]string{"/dist/x.css"}); len(c) != 0 {
		t.Fatalf("unexpected collisions: %v", c)
	}
}

func TestIsParamSegment(t *testing.T) {
	cases := map[string]bool{
		"_id_":         true,
		"_userId_":     true,
		"_a_":          true,
		"users":        false,
		"_id":          false,
		"id_":          false,
		"_":            false,
		"__":           false,
		"_a_b_":        false, // mux regex requires no inner underscore
		"":             false,
	}
	for in, want := range cases {
		if got := isParamSegment(in); got != want {
			t.Errorf("isParamSegment(%q) = %v, want %v", in, got, want)
		}
	}
}

// readAllAndClose reads all of r and closes it.
func readAllAndClose(c interface {
	Read([]byte) (int, error)
	Close() error
}) ([]byte, error) {
	defer c.Close()
	var buf bytes.Buffer
	for {
		chunk := make([]byte, 4096)
		n, err := c.Read(chunk)
		if n > 0 {
			buf.Write(chunk[:n])
		}
		if err != nil {
			if err.Error() == "EOF" {
				return buf.Bytes(), nil
			}
			return buf.Bytes(), err
		}
	}
}
