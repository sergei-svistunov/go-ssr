package mux

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

// limitSite serves /notes, which owns the form "notes.form", with
// MaxBodyBytes set to limit.
func limitSite(t *testing.T, limit int64) (*Mux, *[]string) {
	t.Helper()
	log := &[]string{}
	m := newMux(map[string]Route{"/notes": &fakeRoute{name: "notes", forms: []string{"notes.form"}, log: log}}, Options{MaxBodyBytes: limit})
	return m, log
}

// sizedPost posts the form "notes.form" to /notes, url-encoded and padded to
// exactly size bytes. Without a declared length the body arrives as if
// chunked.
func sizedPost(t *testing.T, size int, declared bool) *http.Request {
	t.Helper()
	req := formRequest(t, http.MethodPost, "/notes", "notes.form", "")
	head, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(head) + "&pad="
	body += strings.Repeat("x", size-len(body))
	req.Body = io.NopCloser(strings.NewReader(body))
	req.ContentLength = int64(len(body))
	if !declared {
		req.ContentLength = -1
	}
	return req
}

func wantTooLarge(t *testing.T, w *http.Response, body string) {
	t.Helper()
	if w.StatusCode != http.StatusRequestEntityTooLarge || body != "Request body too large." {
		t.Errorf("got %d %q, want 413 Request body too large.", w.StatusCode, body)
	}
}

func TestBodyOverLimitIsRefusedBeforeAnyHook(t *testing.T) {
	m, log := limitSite(t, 1024)
	w := serve(m, sizedPost(t, 1025, true))
	wantTooLarge(t, w.Result(), w.Body.String())
	wantLog(t, log, "")
}

func TestUndeclaredBodyOverLimitIsRefusedBeforeAnyFormHook(t *testing.T) {
	m, log := limitSite(t, 1024)
	w := serve(m, sizedPost(t, 1025, false))
	wantTooLarge(t, w.Result(), w.Body.String())
	wantLog(t, log, "notes.guard")
}

func TestBodyAtLimitPasses(t *testing.T) {
	m, log := limitSite(t, 1024)
	for _, declared := range []bool{true, false} {
		*log = nil
		if w := serve(m, sizedPost(t, 1024, declared)); w.Code != http.StatusOK {
			t.Errorf("declared %t: %d %q, want 200", declared, w.Code, w.Body.String())
		}
		wantLog(t, log, "notes.guard notes.init notes.process notes.data")
	}
}

func TestDefaultBodyLimit(t *testing.T) {
	m, _ := limitSite(t, 0)
	if w := serve(m, sizedPost(t, DefaultMaxBodyBytes, true)); w.Code != http.StatusOK {
		t.Errorf("a body of DefaultMaxBodyBytes: %d %q, want 200", w.Code, w.Body.String())
	}
	w := serve(m, sizedPost(t, DefaultMaxBodyBytes+1, true))
	wantTooLarge(t, w.Result(), w.Body.String())
}

func TestNegativeBodyLimitIsRefused(t *testing.T) {
	if _, err := New(nil, Options{CSRFKey: testKey, MaxBodyBytes: -1}); err == nil {
		t.Error("New accepted a negative MaxBodyBytes")
	}
}

// An upload over the limit stops while its file spills to disk, and the
// spilled part is removed.
func TestMultipartOverLimitLeavesNoTempFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	saved := MaxMultipartMemory
	MaxMultipartMemory = 1024
	t.Cleanup(func() { MaxMultipartMemory = saved })

	m, log := limitSite(t, 64<<10)
	signed := formRequest(t, http.MethodPost, "/notes", "notes.form", "")
	fields, err := io.ReadAll(signed.Body)
	if err != nil {
		t.Fatal(err)
	}
	values, err := url.ParseQuery(string(fields))
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField(CSRFFieldName, values.Get(CSRFFieldName)); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("file", "big.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("x"), 128<<10)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := signed.Clone(t.Context())
	req.Body = io.NopCloser(&body)
	req.ContentLength = -1
	req.Header.Set("Content-Type", mw.FormDataContentType())

	w := serve(m, req)
	wantTooLarge(t, w.Result(), w.Body.String())
	wantLog(t, log, "notes.guard")
	if left, err := os.ReadDir(tmp); err != nil || len(left) != 0 {
		t.Errorf("temporary files left: %v %v", left, err)
	}
}
