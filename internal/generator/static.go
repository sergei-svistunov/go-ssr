package generator

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

const (
	staticEmbedDirName = "static_embed"
	staticGenFileName  = "ssrstaticfiles_gen.go"
	manifestName       = ".etags.json"
	manifestTmp        = ".etags.json.tmp"
)

// Skip gzipping for already-compressed formats. Gzip on these costs binary size
// without meaningfully shrinking content.
var skipCompressExt = map[string]bool{
	".png":   true,
	".jpg":   true,
	".jpeg":  true,
	".gif":   true,
	".webp":  true,
	".woff":  true,
	".woff2": true,
	".ico":   true,
	".zip":   true,
	".gz":    true,
	".br":    true,
	".mp4":   true,
	".webm":  true,
	".ogg":   true,
	".mp3":   true,
}

// staticContentTypes are the content types of the files webpack emits. They are
// fixed here rather than read from the host's MIME database, so every host
// generates the same manifest and catalog.
var staticContentTypes = map[string]string{
	".js":    "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".map":   "application/json",
	".json":  "application/json",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".ico":   "image/x-icon",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".txt":   "text/plain; charset=utf-8",
	".html":  "text/html; charset=utf-8",
	".wasm":  "application/wasm",
}

// staticContentType returns the content type for a lower-case extension,
// application/octet-stream for one not in staticContentTypes.
func staticContentType(ext string) string {
	if ct, ok := staticContentTypes[ext]; ok {
		return ct
	}
	return "application/octet-stream"
}

type staticFileEntry struct {
	URLKey      string
	EmbedPath   string
	ContentType string
	ETag        string
	Compressed  bool
	Size        int64
	StoredSize  int64
}

type manifestEntry struct {
	SrcHash     string `json:"srcHash"`
	ETag        string `json:"etag"`
	ContentType string `json:"contentType"`
	Compressed  bool   `json:"compressed"`
	Size        int64  `json:"size"`
	StoredSize  int64  `json:"storedSize"`
}

// genStaticFiles walks the webpack output dir, gzip-precompresses compressible
// files into a staging dir under pages/, and emits a generated Go file with an
// embed.FS catalog plus an HTTP handler. Returns true if any files were
// embedded so the caller can decide whether to wire up the static handler.
//
// Caching: an .etags.json manifest in the staging dir records a SHA-256 of
// each source file's bytes. A file whose hash matches and whose staged copy
// exists is not re-staged. The manifest holds no times, so byte-identical
// sources always produce a byte-identical manifest.
func (g *Generator) genStaticFiles() (bool, []string, error) {
	pagesDir := filepath.Join(g.webDir, "pages")
	embDir := filepath.Join(pagesDir, staticEmbedDirName)
	outFile := filepath.Join(pagesDir, staticGenFileName)

	srcDir := ""
	if g.assets != nil && g.assets.outputPath != "" {
		srcDir = filepath.Join(g.webDir, g.assets.outputPath)
	}

	hasSrc := srcDir != "" && dirExists(srcDir)
	if !hasSrc {
		_ = os.RemoveAll(embDir)
		_ = os.Remove(outFile)
		return false, nil, nil
	}

	emb, err := openStaging(pagesDir)
	if err != nil {
		return false, nil, fmt.Errorf("could not create static embed dir: %w", err)
	}
	defer emb.Close()

	manifest := loadManifest(filepath.Join(embDir, manifestName))
	newManifest := make(map[string]manifestEntry)
	entries := make([]staticFileEntry, 0, 64)

	urlPrefix := "/" + strings.Trim(filepath.ToSlash(g.assets.outputPath), "/") + "/"

	src, err := os.OpenRoot(srcDir)
	if err != nil {
		return false, nil, fmt.Errorf("could not open static dir: %w", err)
	}
	defer src.Close()

	if err := fs.WalkDir(src.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(path.Ext(rel))
		compressible := !skipCompressExt[ext]
		dstRel := rel
		if compressible {
			dstRel += ".gz"
		}
		dstPath := filepath.Join(embDir, filepath.FromSlash(dstRel))

		ct := staticContentType(ext)

		urlKey := urlPrefix + rel

		raw, err := src.ReadFile(filepath.FromSlash(rel))
		if err != nil {
			return err
		}
		srcSum := sha256.Sum256(raw)
		srcHash := hex.EncodeToString(srcSum[:])

		if cached, ok := manifest[dstRel]; ok && cached.SrcHash == srcHash && fileExists(dstPath) {
			cached.ContentType = ct
			newManifest[dstRel] = cached
			entries = append(entries, staticFileEntry{
				URLKey:      urlKey,
				EmbedPath:   path.Join(staticEmbedDirName, dstRel),
				ContentType: cached.ContentType,
				ETag:        cached.ETag,
				Compressed:  cached.Compressed,
				Size:        cached.Size,
				StoredSize:  cached.StoredSize,
			})
			return nil
		}

		var stored []byte
		if compressible {
			var buf bytes.Buffer
			gz, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			if err != nil {
				return err
			}
			if _, err := gz.Write(raw); err != nil {
				_ = gz.Close()
				return err
			}
			if err := gz.Close(); err != nil {
				return err
			}
			stored = buf.Bytes()
		} else {
			stored = raw
		}

		if err := emb.MkdirAll(filepath.Dir(filepath.FromSlash(dstRel)), 0o755); err != nil {
			return err
		}
		if err := emb.WriteFile(filepath.FromSlash(dstRel), stored, 0o644); err != nil {
			return err
		}

		sum := sha256.Sum256(stored)
		etag := `"` + hex.EncodeToString(sum[:8]) + `"`

		size := int64(len(raw))
		storedSize := int64(len(stored))

		newManifest[dstRel] = manifestEntry{
			SrcHash:     srcHash,
			ETag:        etag,
			ContentType: ct,
			Compressed:  compressible,
			Size:        size,
			StoredSize:  storedSize,
		}
		entries = append(entries, staticFileEntry{
			URLKey:      urlKey,
			EmbedPath:   path.Join(staticEmbedDirName, dstRel),
			ContentType: ct,
			ETag:        etag,
			Compressed:  compressible,
			Size:        size,
			StoredSize:  storedSize,
		})
		return nil
	}); err != nil {
		return false, nil, fmt.Errorf("could not walk static dir: %w", err)
	}

	if err := pruneStaging(emb, newManifest); err != nil {
		return false, nil, fmt.Errorf("could not prune static embed dir: %w", err)
	}

	if err := saveManifest(emb, newManifest); err != nil {
		return false, nil, fmt.Errorf("could not write static embed manifest: %w", err)
	}

	if len(entries) == 0 {
		_ = os.Remove(outFile)
		return false, nil, nil
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].URLKey < entries[j].URLKey })

	urlKeys := make([]string, len(entries))
	for i, e := range entries {
		urlKeys[i] = e.URLKey
	}

	if err := writeStaticGo(pagesDir, entries); err != nil {
		return false, nil, err
	}
	return true, urlKeys, nil
}

func writeStaticGo(pagesDir string, entries []staticFileEntry) error {
	buf := gobuf.New()
	buf.WriteStringLn(goFileHeader)
	buf.WriteStringLn("package pages")

	buf.WriteStringLn("import (")
	buf.WriteQuotedString("embed", "\n")
	buf.WriteQuotedString("net/http", "\n\n")
	buf.WriteQuotedString("github.com/sergei-svistunov/go-ssr/pkg/static", "\n")
	buf.WriteStringLn(")")

	buf.WriteStringLn("//go:embed all:" + staticEmbedDirName)
	buf.WriteStringLn("var ssrStaticFS embed.FS")

	buf.WriteStringLn("var ssrStaticFiles = map[string]static.File{")
	for _, e := range entries {
		buf.WriteQuotedString(e.URLKey, ": {")
		buf.WriteString("ContentType: ")
		buf.WriteQuotedString(e.ContentType, ", ")
		buf.WriteString("ETag: ")
		buf.WriteQuotedString(e.ETag, ", ")
		buf.WriteString("EmbedPath: ")
		buf.WriteQuotedString(e.EmbedPath, ", ")
		if e.Compressed {
			buf.WriteString("Compressed: true, ")
		}
		buf.WriteString(fmt.Sprintf("Size: %d, StoredSize: %d", e.Size, e.StoredSize))
		buf.WriteStringLn("},")
	}
	buf.WriteStringLn("}")

	buf.WriteStringLn("func ssrServeStatic(w http.ResponseWriter, r *http.Request) bool {")
	buf.WriteStringLn("return static.Serve(w, r, ssrStaticFS, ssrStaticFiles)")
	buf.WriteStringLn("}")

	formatted, err := buf.Formatted()
	if err != nil {
		return fmt.Errorf("could not format %s: %w\n%s", staticGenFileName, err, buf.String())
	}
	return writeFile(pagesDir, staticGenFileName, formatted)
}

// openStaging creates the staging dir in pagesDir and opens it as a root.
func openStaging(pagesDir string) (*os.Root, error) {
	pages, err := os.OpenRoot(pagesDir)
	if err != nil {
		return nil, err
	}
	defer pages.Close()
	if err := pages.MkdirAll(staticEmbedDirName, 0o755); err != nil {
		return nil, err
	}
	return pages.OpenRoot(staticEmbedDirName)
}

func loadManifest(p string) map[string]manifestEntry {
	data, err := os.ReadFile(p)
	if err != nil {
		return map[string]manifestEntry{}
	}
	var m map[string]manifestEntry
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]manifestEntry{}
	}
	return m
}

func saveManifest(emb *os.Root, m map[string]manifestEntry) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := emb.WriteFile(manifestTmp, data, 0o644); err != nil {
		return err
	}
	return emb.Rename(manifestTmp, manifestName)
}

// pruneStaging removes the files of emb that keep does not list, then the
// folders that leaves empty.
func pruneStaging(emb *os.Root, keep map[string]manifestEntry) error {
	dirs := make([]string, 0)
	if err := fs.WalkDir(emb.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." {
				dirs = append(dirs, p)
			}
			return nil
		}
		if p == manifestName || p == manifestTmp {
			return nil
		}
		if _, ok := keep[p]; ok {
			return nil
		}
		return emb.Remove(filepath.FromSlash(p))
	}); err != nil {
		return err
	}
	// Post-order: remove empty dirs left behind by orphan removal. Sort
	// descending by length so children are visited before parents.
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		_ = emb.Remove(filepath.FromSlash(d)) // ignore ENOTEMPTY
	}
	return nil
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
