// Package static is the runtime support for the embedded static-file handler
// emitted by go-ssr. The generated code defines an embed.FS and a map of
// File entries, then delegates request handling to Serve.
package static

import (
	"compress/gzip"
	"embed"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// File describes a single embedded static asset.
type File struct {
	ContentType string
	ETag        string
	EmbedPath   string
	Compressed  bool
	Size        int64 // uncompressed size
	StoredSize  int64 // size of the on-disk embedded payload
}

// Serve looks up r.URL.Path in files; if absent, returns false so the caller
// can fall through to its next handler. On a hit it writes the response and
// returns true.
func Serve(w http.ResponseWriter, r *http.Request, fs embed.FS, files map[string]File) bool {
	f, ok := files[r.URL.Path]
	if !ok {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return true
	}

	w.Header().Set("Content-Type", f.ContentType)
	w.Header().Set("ETag", f.ETag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if f.Compressed {
		w.Header().Set("Vary", "Accept-Encoding")
	}

	if ifNoneMatch(r.Header.Get("If-None-Match"), f.ETag) {
		w.WriteHeader(http.StatusNotModified)
		return true
	}

	acceptsGz := AcceptsGzip(r.Header.Get("Accept-Encoding"))
	decompressing := f.Compressed && !acceptsGz

	if f.Compressed && acceptsGz {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", strconv.FormatInt(f.StoredSize, 10))
	} else if !f.Compressed {
		w.Header().Set("Content-Length", strconv.FormatInt(f.Size, 10))
	}
	// When decompressing on the fly, Content-Length is intentionally omitted —
	// the manifest's Size could disagree with the actual decompressed length if
	// the staged file ever drifts (e.g. cache corruption), and a length
	// mismatch breaks the response. Let the http server fall back to chunked.

	if r.Method == http.MethodHead {
		return true
	}

	file, err := fs.Open(f.EmbedPath)
	if err != nil {
		http.Error(w, "static read failed", http.StatusInternalServerError)
		return true
	}
	defer file.Close()

	if !decompressing {
		_, _ = io.Copy(w, file)
		return true
	}
	gzr, err := gzip.NewReader(file)
	if err != nil {
		http.Error(w, "static decompress failed", http.StatusInternalServerError)
		return true
	}
	defer gzr.Close()
	_, _ = io.Copy(w, gzr)
	return true
}

// ifNoneMatch reports whether the request's If-None-Match header indicates a
// match against the resource's ETag. Per RFC 9110 §13.1.2 it accepts:
//   - "*" (matches any existing resource)
//   - a comma-separated list of entity-tags
//   - weak ("W/"-prefixed) tags, where weak comparison is used (the spec
//     permits weak ETags here).
func ifNoneMatch(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	target := stripWeakPrefix(etag)
	for _, tok := range strings.Split(header, ",") {
		if stripWeakPrefix(strings.TrimSpace(tok)) == target {
			return true
		}
	}
	return false
}

func stripWeakPrefix(t string) string {
	if strings.HasPrefix(t, "W/") {
		return t[2:]
	}
	return t
}

// AcceptsGzip parses an Accept-Encoding header and returns true iff the client
// will accept gzip. Tokens with q=0 are treated as a refusal; "*" matches any
// coding not explicitly listed. Per RFC 9110 §12.5.3, an explicit "gzip;q=0"
// overrides "*".
func AcceptsGzip(h string) bool {
	if h == "" {
		return false
	}
	var gzipExplicit, gzipRefused, starOK bool
	for _, part := range strings.Split(h, ",") {
		part = strings.TrimSpace(part)
		coding := part
		params := ""
		if i := strings.IndexByte(part, ';'); i >= 0 {
			coding = strings.TrimSpace(part[:i])
			params = part[i+1:]
		}
		switch strings.ToLower(coding) {
		case "gzip":
			if qIsZero(params) {
				gzipRefused = true
			} else {
				gzipExplicit = true
			}
		case "*":
			if !qIsZero(params) {
				starOK = true
			}
		}
	}
	if gzipRefused {
		return false
	}
	return gzipExplicit || starOK
}

func qIsZero(params string) bool {
	for _, prm := range strings.Split(params, ";") {
		prm = strings.TrimSpace(strings.ToLower(prm))
		if !strings.HasPrefix(prm, "q=") {
			continue
		}
		v := strings.TrimSpace(prm[2:])
		f, err := strconv.ParseFloat(v, 64)
		if err == nil && f <= 0 {
			return true
		}
	}
	return false
}
