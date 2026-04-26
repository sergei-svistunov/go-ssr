package static

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io"
	"net/http/httptest"
	"testing"
)

//go:embed testdata
var testFS embed.FS

func TestAcceptsGzip(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{"", false},
		{"gzip", true},
		{"gzip, deflate", true},
		{"deflate, gzip", true},
		{"deflate, gzip;q=0.5", true},
		{"gzip;q=0", false},
		{"gzip;q=0.0", false},
		{"deflate;q=1, gzip;q=0", false},
		{"x-gzip", false},
		{"deflate", false},
		{"*", true},
		{"*;q=0", false},
		{"GZIP", true},
		{"gzip ; q = 1", true},
		{"gzip;q=0, *", false},
		{"*, gzip;q=0", false},
		{"identity, *;q=0, gzip", true},
	}
	for _, c := range cases {
		if got := AcceptsGzip(c.header); got != c.want {
			t.Errorf("AcceptsGzip(%q) = %v, want %v", c.header, got, c.want)
		}
	}
}

func TestServe_Miss(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/missing", nil)
	if Serve(rec, req, embed.FS{}, nil) {
		t.Fatal("expected miss to return false")
	}
}

func TestServe_MethodNotAllowed(t *testing.T) {
	files := map[string]File{
		"/x.css": {ContentType: "text/css", ETag: `"e"`, EmbedPath: "x", Size: 5},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/x.css", nil)
	if !Serve(rec, req, embed.FS{}, files) {
		t.Fatal("expected handled=true")
	}
	if rec.Code != 405 {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if rec.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("Allow header = %q", rec.Header().Get("Allow"))
	}
}

func TestServe_MethodNotAllowedBeatsIfNoneMatch(t *testing.T) {
	files := map[string]File{
		"/x.css": {ContentType: "text/css", ETag: `"e"`, EmbedPath: "x", Size: 5},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/x.css", nil)
	req.Header.Set("If-None-Match", `"e"`)
	if !Serve(rec, req, embed.FS{}, files) {
		t.Fatal("expected handled=true")
	}
	if rec.Code != 405 {
		t.Fatalf("status = %d, want 405 (must precede 304)", rec.Code)
	}
}

func TestServe_NotModified(t *testing.T) {
	files := map[string]File{
		"/x.css": {ContentType: "text/css", ETag: `"e"`, EmbedPath: "x", Size: 5},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x.css", nil)
	req.Header.Set("If-None-Match", `"e"`)
	if !Serve(rec, req, embed.FS{}, files) {
		t.Fatal("expected handled=true")
	}
	if rec.Code != 304 {
		t.Fatalf("status = %d, want 304", rec.Code)
	}
	if rec.Header().Get("ETag") != `"e"` {
		t.Fatalf("ETag missing on 304 response")
	}
	if rec.Header().Get("Cache-Control") == "" {
		t.Fatalf("Cache-Control missing on 304 response")
	}
}

func TestIfNoneMatch(t *testing.T) {
	cases := []struct {
		header, etag string
		want         bool
	}{
		{"", `"abc"`, false},
		{`"abc"`, `"abc"`, true},
		{`"def"`, `"abc"`, false},
		{`*`, `"abc"`, true},
		{`*`, ``, true}, // wildcard matches any extant resource
		{`"abc", "def"`, `"def"`, true},
		{`"abc", "def"`, `"xyz"`, false},
		{`W/"abc"`, `"abc"`, true},
		{`"abc"`, `W/"abc"`, true},
		{`W/"abc"`, `W/"abc"`, true},
		{`W/"abc", "def"`, `"def"`, true},
		{`  "abc"  ,  "def"  `, `"abc"`, true},
	}
	for _, c := range cases {
		if got := ifNoneMatch(c.header, c.etag); got != c.want {
			t.Errorf("ifNoneMatch(%q, %q) = %v, want %v", c.header, c.etag, got, c.want)
		}
	}
}

func TestServe_NotModifiedWithList(t *testing.T) {
	files := map[string]File{
		"/x.css": {ContentType: "text/css", ETag: `"e"`, EmbedPath: "x", Size: 5},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x.css", nil)
	req.Header.Set("If-None-Match", `"a", "e", "b"`)
	if !Serve(rec, req, embed.FS{}, files) {
		t.Fatal("expected handled=true")
	}
	if rec.Code != 304 {
		t.Fatalf("status = %d, want 304", rec.Code)
	}
}

func TestServe_NotModifiedWithWildcard(t *testing.T) {
	files := map[string]File{
		"/x.css": {ContentType: "text/css", ETag: `"e"`, EmbedPath: "x", Size: 5},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x.css", nil)
	req.Header.Set("If-None-Match", "*")
	if !Serve(rec, req, embed.FS{}, files) {
		t.Fatal("expected handled=true")
	}
	if rec.Code != 304 {
		t.Fatalf("status = %d, want 304", rec.Code)
	}
}

func TestServe_CompressedPassthrough(t *testing.T) {
	files := map[string]File{
		"/c.txt": {ContentType: "text/plain", ETag: `"e"`, EmbedPath: "testdata/comp.txt.gz", Compressed: true, Size: 16, StoredSize: 36},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/c.txt", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	if !Serve(rec, req, testFS, files) {
		t.Fatal("expected handled=true")
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := rec.Header().Get("Content-Length"); got != "36" {
		t.Fatalf("Content-Length = %q, want 36", got)
	}
	if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("Vary = %q, want Accept-Encoding", got)
	}
	if rec.Body.Len() != 36 {
		t.Fatalf("body len = %d, want 36 (verbatim gzip stream)", rec.Body.Len())
	}
	gzr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("body is not valid gzip: %v", err)
	}
	got, _ := io.ReadAll(gzr)
	if string(got) != "hello compressed" {
		t.Fatalf("decompressed body = %q", got)
	}
}

func TestServe_CompressedDecompressForNonGzipClient(t *testing.T) {
	files := map[string]File{
		"/c.txt": {ContentType: "text/plain", ETag: `"e"`, EmbedPath: "testdata/comp.txt.gz", Compressed: true, Size: 16, StoredSize: 36},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/c.txt", nil)
	if !Serve(rec, req, testFS, files) {
		t.Fatal("expected handled=true")
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}
	if got := rec.Header().Get("Content-Length"); got != "" {
		t.Fatalf("Content-Length = %q, want empty (chunked)", got)
	}
	if rec.Body.String() != "hello compressed" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestServe_HeadOnCompressed(t *testing.T) {
	files := map[string]File{
		"/c.txt": {ContentType: "text/plain", ETag: `"e"`, EmbedPath: "testdata/comp.txt.gz", Compressed: true, Size: 16, StoredSize: 36},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("HEAD", "/c.txt", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	if !Serve(rec, req, testFS, files) {
		t.Fatal("expected handled=true")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD body must be empty, got %d bytes", rec.Body.Len())
	}
	if got := rec.Header().Get("Content-Length"); got != "36" {
		t.Fatalf("Content-Length = %q, want 36", got)
	}
}

func TestServe_PlainGet(t *testing.T) {
	files := map[string]File{
		"/p.txt": {ContentType: "text/plain", ETag: `"e"`, EmbedPath: "testdata/plain.txt", Size: 11},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/p.txt", nil)
	if !Serve(rec, req, testFS, files) {
		t.Fatal("expected handled=true")
	}
	if got := rec.Header().Get("Content-Length"); got != "11" {
		t.Fatalf("Content-Length = %q, want 11", got)
	}
	if rec.Body.String() != "hello plain" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestServe_EmptyFileSetsContentLengthZero(t *testing.T) {
	files := map[string]File{
		"/e.txt": {ContentType: "text/plain", ETag: `"e"`, EmbedPath: "testdata/empty.txt", Size: 0},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/e.txt", nil)
	if !Serve(rec, req, testFS, files) {
		t.Fatal("expected handled=true")
	}
	if got := rec.Header().Get("Content-Length"); got != "0" {
		t.Fatalf("Content-Length = %q, want 0", got)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body must be empty")
	}
}
