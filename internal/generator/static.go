package generator

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

const (
	staticEmbedDirName = "static_embed"
	staticGenFileName  = "ssrstaticfiles_gen.go"
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
	Mtime       int64  `json:"mtime"`
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
// Caching: an .etags.json manifest in the staging dir tracks per-file mtime
// AND an MD5 of the source bytes. The fast path skips re-staging when both
// match. Hashing the source is needed because mtime alone is not reliable —
// git checkout, rsync -a, tar -p, unzip -X and Docker COPY all preserve
// mtimes, so an unchanged mtime does not guarantee unchanged content.
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

	if err := os.MkdirAll(embDir, 0755); err != nil {
		return false, nil, fmt.Errorf("could not create static embed dir: %w", err)
	}

	manifest := loadManifest(filepath.Join(embDir, ".etags.json"))
	newManifest := make(map[string]manifestEntry)
	entries := make([]staticFileEntry, 0, 64)

	urlPrefix := "/" + strings.Trim(filepath.ToSlash(g.assets.outputPath), "/") + "/"

	if err := filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(srcDir, p)
		if err != nil {
			return err
		}
		if runtime.GOOS == "windows" {
			rel = strings.ReplaceAll(rel, "\\", "/")
		}

		ext := strings.ToLower(path.Ext(rel))
		compressible := !skipCompressExt[ext]
		dstRel := rel
		if compressible {
			dstRel += ".gz"
		}
		dstPath := filepath.Join(embDir, filepath.FromSlash(dstRel))

		srcInfo, err := os.Stat(p)
		if err != nil {
			return err
		}
		srcMtime := srcInfo.ModTime().UnixNano()

		ct := mime.TypeByExtension(ext)
		if ct == "" {
			ct = "application/octet-stream"
		}

		urlKey := urlPrefix + rel

		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		srcSum := md5.Sum(raw)
		srcHash := hex.EncodeToString(srcSum[:])

		if cached, ok := manifest[dstRel]; ok && cached.SrcHash == srcHash && fileExists(dstPath) {
			cached.Mtime = srcMtime
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

		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(dstPath, stored, 0644); err != nil {
			return err
		}

		sum := md5.Sum(stored)
		etag := `"` + hex.EncodeToString(sum[:8]) + `"`

		size := int64(len(raw))
		storedSize := int64(len(stored))

		newManifest[dstRel] = manifestEntry{
			Mtime:       srcMtime,
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

	if err := pruneStaging(embDir, newManifest); err != nil {
		return false, nil, fmt.Errorf("could not prune static embed dir: %w", err)
	}

	if err := saveManifest(filepath.Join(embDir, ".etags.json"), newManifest); err != nil {
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

	if err := writeStaticGo(outFile, entries); err != nil {
		return false, nil, err
	}
	return true, urlKeys, nil
}

func writeStaticGo(outFile string, entries []staticFileEntry) error {
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
	return os.WriteFile(outFile, formatted, 0644)
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

func saveManifest(p string, m map[string]manifestEntry) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func pruneStaging(embDir string, keep map[string]manifestEntry) error {
	manifestName := ".etags.json"
	manifestTmp := ".etags.json.tmp"
	dirs := make([]string, 0)
	if err := filepath.WalkDir(embDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != embDir {
				dirs = append(dirs, p)
			}
			return nil
		}
		rel, err := filepath.Rel(embDir, p)
		if err != nil {
			return err
		}
		base := filepath.Base(p)
		if base == manifestName || base == manifestTmp {
			return nil
		}
		key := rel
		if runtime.GOOS == "windows" {
			key = strings.ReplaceAll(key, "\\", "/")
		}
		if _, ok := keep[key]; ok {
			return nil
		}
		return os.Remove(p)
	}); err != nil {
		return err
	}
	// Post-order: remove empty dirs left behind by orphan removal. Sort
	// descending by length so children are visited before parents.
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		_ = os.Remove(d) // ignore ENOTEMPTY
	}
	return nil
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
