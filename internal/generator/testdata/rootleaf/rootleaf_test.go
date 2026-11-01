package app_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"example.com/app/calls"
	"example.com/app/web/pages"
	"example.com/app/web/pages/child"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var tokenField = regexp.MustCompile(`name="_csrf_token_" value="item:([^"]+)"`)

func TestRootToLeaf(t *testing.T) {
	h, err := pages.NewSsrHandler(mux.Options{CSRFKey: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/child", nil))
	m := tokenField.FindStringSubmatch(w.Body.String())
	if w.Code != http.StatusOK || m == nil || len(w.Result().Cookies()) != 1 {
		t.Fatalf("GET /child = %d %q, want 200 with a form token and a cookie", w.Code, w.Body.String())
	}
	if got, want := calls.Take(), "root.guard child.guard root.init child.init root.data child.data"; got != want {
		t.Errorf("GET: hooks ran as %q, want %q", got, want)
	}
	cookie, token := w.Result().Cookies()[0], m[1]

	post := func(target, form string, fields url.Values) int {
		fields.Set("_csrf_token_", form+":"+token)
		req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(fields.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}

	for _, tc := range []struct {
		name, target, form string
		fields             url.Values
		code               int
		want               string
	}{
		{"child form", "/child", "item", url.Values{"name": {"x"}}, http.StatusOK,
			"root.guard child.guard root.init child.init child.process root.data child.data"},
		{"root form", "/child", "note", url.Values{"text": {"x"}}, http.StatusOK,
			"root.guard child.guard root.init child.init root.process root.data child.data"},
		{"required field empty", "/child", "item", url.Values{"name": {""}}, http.StatusOK,
			"root.guard child.guard root.init child.init root.data child.data"},
		{"root guard refuses", "/child?refuse=1", "item", url.Values{"name": {"x"}}, http.StatusNotFound,
			"root.guard"},
	} {
		if code := post(tc.target, tc.form, tc.fields); code != tc.code {
			t.Errorf("%s: status %d, want %d", tc.name, code, tc.code)
		}
		if got := calls.Take(); got != tc.want {
			t.Errorf("%s: hooks ran as %q, want %q", tc.name, got, tc.want)
		}
	}
}

type misspeltGuard struct{ *child.DP }

func (misspeltGuard) Guard(context.Context, *mux.Request, mux.ResponseWriter) error { return nil }

func TestNewRouteRefusesAMisspeltGuard(t *testing.T) {
	defer func() {
		if msg := fmt.Sprint(recover()); !strings.Contains(msg, "E21") || !strings.Contains(msg, "/child") {
			t.Errorf("panic = %q, want E21 naming /child", msg)
		}
	}()
	child.NewRoute(misspeltGuard{child.NewDP()})
}
