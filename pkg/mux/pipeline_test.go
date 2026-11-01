package mux

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// fakeRoute logs "<name>.<hook>" into a log shared by the whole site and
// renders as <name>…</name> around its child.
type fakeRoute struct {
	name    string
	log     *[]string
	layout  bool
	def     string
	forms   []string
	invalid bool              // a submitted form fails validation, so it is not processed
	fail    map[string]error  // hook name → the error that hook returns
	params  map[string]string // the URL parameters Guard last saw
	header  http.Header       // headers Data sets on the response
}

func (f *fakeRoute) NewState() RouteState { return &fakeState{route: f} }

func (f *fakeRoute) Layout() bool { return f.layout }

type fakeState struct {
	Frame
	route *fakeRoute
}

func (s *fakeState) hook(name string) error {
	*s.route.log = append(*s.route.log, s.route.name+"."+name)
	return s.route.fail[name]
}

func (s *fakeState) Guard(_ context.Context, r *Request) error {
	s.route.params = maps.Clone(r.URLParams)
	return s.hook("guard")
}

func (s *fakeState) InitForms(context.Context, *Request, ResponseWriter) error {
	return s.hook("init")
}

func (s *fakeState) SubmitForm(_ context.Context, _ *Request, _ ResponseWriter, id string) (bool, error) {
	if !slices.Contains(s.route.forms, id) {
		return false, nil
	}
	if s.route.invalid {
		return true, nil
	}
	return true, s.hook("process")
}

func (s *fakeState) Data(_ context.Context, _ *Request, w ResponseWriter) error {
	maps.Copy(w.Header(), s.route.header)
	return s.hook("data")
}

func (s *fakeState) DefaultRoute(context.Context, *Request) (string, error) {
	return s.route.def, nil
}

func (s *fakeState) Write(w io.Writer) error {
	if err := s.route.fail["write"]; err != nil {
		return err
	}
	if _, err := io.WriteString(w, "<"+s.route.name+">"); err != nil {
		return err
	}
	if err := s.WriteChild(w); err != nil {
		return err
	}
	_, err := io.WriteString(w, "</"+s.route.name+">")
	return err
}

// site builds the test site; change adjusts its routes before the mux is made.
func site(t *testing.T, change func(map[string]*fakeRoute)) (*Mux, *[]string) {
	t.Helper()
	log := &[]string{}
	routes := map[string]*fakeRoute{
		"/":             {name: "root", layout: true, def: "home"},
		"/home":         {name: "home"},
		"/notes":        {name: "notes", forms: []string{"notes.form"}},
		"/notes/_id_":   {name: "item", forms: []string{"item.form"}},
		"/admin":        {name: "admin", layout: true, def: "panel"},
		"/admin/panel":  {name: "panel", forms: []string{"panel.form"}},
		"/note/_id_":    {name: "note", layout: true},
		"/note/_id_/ed": {name: "ed", forms: []string{"ed.form"}},
	}
	if change != nil {
		change(routes)
	}
	m := map[string]Route{}
	for p, r := range routes {
		r.log = log
		m[p] = r
	}
	return newMux(m, Options{CSRFSession: func(r *Request) string { return r.Header.Get("Session") }}), log
}

// formRequest posts form id to path with a cookie and a field signed for
// session.
func formRequest(t testing.TB, method, path, id, session string) *http.Request {
	signer := NewRequest(withOptions(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), requestOptions{
		csrfKey:       testKey,
		csrfSessionFn: func(*Request) string { return session },
	}))
	token, err := mintToken(signer)
	if err != nil {
		panic(err)
	}
	body := url.Values{CSRFFieldName: {id + ":" + token}}.Encode()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
	return req
}

func serve(m *Mux, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	m.ServeHTTP(w, req)
	return w
}

func wantLog(t *testing.T, log *[]string, want string) {
	t.Helper()
	if got := strings.Join(*log, " "); got != want {
		t.Errorf("hooks ran as\n  %s\nwant\n  %s", got, want)
	}
}

func TestPageRendersRootToLeaf(t *testing.T) {
	m, log := site(t, nil)
	w := doGet(t, m, "/home")
	if w.Code != http.StatusOK || w.Body.String() != "<root><home></home></root>" {
		t.Errorf("GET /home = %d %q, want 200 <root><home></home></root>", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	wantSecurityHeaders(t, w.Header(), strings.Replace(testCSP, " ws://example.test", " ws://example.com", 1))
	wantLog(t, log, "root.guard home.guard root.init home.init root.data home.data")
}

func TestPostRunsProcessAfterChecks(t *testing.T) {
	m, log := site(t, nil)
	if w := serve(m, formRequest(t, http.MethodPost, "/notes", "notes.form", "")); w.Code != http.StatusOK {
		t.Errorf("status %d, want 200", w.Code)
	}
	wantLog(t, log, "root.guard notes.guard root.init notes.init notes.process root.data notes.data")
}

func TestPostUnderDeniedParentNeverProcesses(t *testing.T) {
	m, log := site(t, func(r map[string]*fakeRoute) {
		r["/admin"].fail = map[string]error{"guard": NewHttpError(http.StatusForbidden, "Forbidden")}
	})
	if w := serve(m, formRequest(t, http.MethodPost, "/admin/panel", "panel.form", "")); w.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403", w.Code)
	}
	wantLog(t, log, "root.guard admin.guard")

	m, log = site(t, func(r map[string]*fakeRoute) {
		r["/note/_id_"].fail = map[string]error{"guard": NotFound()}
	})
	if w := serve(m, formRequest(t, http.MethodPost, "/note/7/ed", "ed.form", "")); w.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", w.Code)
	}
	wantLog(t, log, "root.guard note.guard")
}

func TestTokenIsCheckedBeforeInit(t *testing.T) {
	m, log := site(t, nil)
	if w := serve(m, formRequest(t, http.MethodPost, "/notes", "notes.form", "someone-else")); w.Code != http.StatusBadRequest {
		t.Errorf("token for another session: status %d, want 400", w.Code)
	}
	wantLog(t, log, "root.guard notes.guard")

	m, log = site(t, nil)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/notes", strings.NewReader("note=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if w := serve(m, req); w.Code != http.StatusBadRequest {
		t.Errorf("no token: status %d, want 400", w.Code)
	}
	wantLog(t, log, "root.guard notes.guard")

	m, log = site(t, func(r map[string]*fakeRoute) { r["/notes"].invalid = true })
	if w := serve(m, formRequest(t, http.MethodPost, "/notes", "notes.form", "")); w.Code != http.StatusOK {
		t.Errorf("invalid form: status %d, want 200", w.Code)
	}
	if slices.Contains(*log, "notes.process") {
		t.Errorf("an invalid form was processed: %v", *log)
	}
}

func TestOnlyTheSubmittedFormIsProcessed(t *testing.T) {
	m, log := site(t, func(r map[string]*fakeRoute) { r["/"].forms = []string{"root.form"} })
	serve(m, formRequest(t, http.MethodPost, "/notes", "notes.form", ""))
	if !slices.Contains(*log, "notes.process") || slices.Contains(*log, "root.process") {
		t.Errorf("want notes.process and no root.process, got %v", *log)
	}
	if !slices.Contains(*log, "root.init") || !slices.Contains(*log, "notes.init") {
		t.Errorf("want both inits, got %v", *log)
	}

	m, log = site(t, func(r map[string]*fakeRoute) {
		r["/"].forms = []string{"edit"}
		r["/notes"].forms = []string{"edit"}
	})
	serve(m, formRequest(t, http.MethodPost, "/notes", "edit", ""))
	if !slices.Contains(*log, "root.process") || slices.Contains(*log, "notes.process") {
		t.Errorf("two routes own the form: want only the first one's process, got %v", *log)
	}
}

func TestUnknownFormIsRefused(t *testing.T) {
	m, log := site(t, nil)
	w := serve(m, formRequest(t, http.MethodPost, "/notes", "other.form", ""))
	if w.Code != http.StatusBadRequest || w.Body.String() != "This form is not on this page." {
		t.Errorf("got %d %q, want 400 This form is not on this page.", w.Code, w.Body.String())
	}
	for _, entry := range *log {
		if strings.HasSuffix(entry, ".process") {
			t.Errorf("%s ran for a form no route owns", entry)
		}
	}
}

func TestOtherMethodsAreRefused(t *testing.T) {
	for _, method := range []string{http.MethodDelete, http.MethodOptions, http.MethodPut} {
		m, log := site(t, nil)
		w := serve(m, formRequest(t, method, "/notes", "notes.form", ""))
		if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD, POST" {
			t.Errorf("%s: got %d Allow %q, want 405 GET, HEAD, POST", method, w.Code, w.Header().Get("Allow"))
		}
		wantLog(t, log, "")
	}
}

func TestGuardRedirects(t *testing.T) {
	m, log := site(t, func(r map[string]*fakeRoute) {
		r["/"].fail = map[string]error{"guard": Redirect(http.StatusFound, "/login")}
	})
	w := doGet(t, m, "/home")
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/login" {
		t.Errorf("got %d Location %q, want 302 /login", w.Code, w.Header().Get("Location"))
	}
	wantLog(t, log, "root.guard")
}

func TestParentRedirectsAfterGuards(t *testing.T) {
	m, log := site(t, nil)
	w := doGet(t, m, "/admin")
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/admin/panel" {
		t.Errorf("got %d Location %q, want 302 /admin/panel", w.Code, w.Header().Get("Location"))
	}
	wantLog(t, log, "root.guard admin.guard")

	m, _ = site(t, func(r map[string]*fakeRoute) {
		r["/admin"].fail = map[string]error{"guard": NewHttpError(http.StatusForbidden, "Forbidden")}
	})
	w = doGet(t, m, "/admin")
	if w.Code != http.StatusForbidden || w.Header().Get("Location") != "" {
		t.Errorf("denied: got %d Location %q, want 403 and no redirect", w.Code, w.Header().Get("Location"))
	}

	m, _ = site(t, nil)
	w = serve(m, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/admin", nil))
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST: got %d Allow %q, want 405 GET, HEAD", w.Code, w.Header().Get("Allow"))
	}

	for _, def := range []string{"", ".", "../admin"} {
		m, _ = site(t, func(r map[string]*fakeRoute) { r["/admin"].def = def })
		if w := doGet(t, m, "/admin"); w.Code != http.StatusNotFound {
			t.Errorf("default %q: got %d Location %q, want 404", def, w.Code, w.Header().Get("Location"))
		}
	}
}

func TestFailedTemplateSendsNoHalfPage(t *testing.T) {
	m, _ := site(t, func(r map[string]*fakeRoute) {
		r["/home"].fail = map[string]error{"write": errors.New("template failed")}
	})
	captureRefusalLog(t)
	w := doGet(t, m, "/home")
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "<root>") {
		t.Errorf("got %d %q, want the error handler's 500 and no part of the page", w.Code, w.Body.String())
	}
}

// Each <ssr:assets/> on a page's path is one WriteAssets call; together they
// write every tag once, whichever renders first.
func TestFrameWritesEachAssetOncePerResponse(t *testing.T) {
	for _, tc := range []struct {
		order []int // 0 the root layout, 1 the nested layout
		want  []string
	}{
		{[]int{0, 1}, []string{"<a><b><c><d>", ""}},
		{[]int{1, 0}, []string{"<b><c><d>", "<a>"}},
	} {
		root, layout, leaf := &fakeState{}, &fakeState{}, &fakeState{}
		root.SetAssets([]string{"<a>", "<b>"})
		layout.SetAssets([]string{"<b>", "<c>"})
		leaf.SetAssets([]string{"<c>", "<d>"})
		root.SetChild(layout)
		layout.SetChild(leaf)
		for i, s := range tc.order {
			var b strings.Builder
			if err := []*fakeState{root, layout}[s].WriteAssets(&b); err != nil || b.String() != tc.want[i] {
				t.Errorf("order %v: call %d wrote %q, %v; want %q", tc.order, i+1, b.String(), err, tc.want[i])
			}
		}
	}

	page := &fakeState{} // a single-route page: SetChild never runs
	page.SetAssets([]string{"<a>"})
	for i, want := range []string{"<a>", ""} {
		var b strings.Builder
		if err := page.WriteAssets(&b); err != nil || b.String() != want {
			t.Errorf("single route: call %d wrote %q, %v; want %q", i+1, b.String(), err, want)
		}
	}
}

func wantPage(t *testing.T, w *httptest.ResponseRecorder, want string) {
	t.Helper()
	if w.Code != http.StatusOK || w.Body.String() != want {
		t.Errorf("got %d %q, want 200 %q", w.Code, w.Body.String(), want)
	}
}

func TestGateRendersItsOwnPage(t *testing.T) {
	m, _ := site(t, nil)
	wantPage(t, doGet(t, m, "/notes"), "<root><notes></notes></root>")

	m, log := site(t, nil)
	wantPage(t, doGet(t, m, "/notes/7"), "<root><item></item></root>")
	wantLog(t, log, "root.guard notes.guard item.guard root.init item.init root.data item.data")

	m, _ = site(t, nil)
	if w := serve(m, httptest.NewRequestWithContext(t.Context(), http.MethodHead, "/notes", nil)); w.Code != http.StatusOK {
		t.Errorf("HEAD /notes = %d, want 200", w.Code)
	}

	m, log = site(t, nil)
	serve(m, formRequest(t, http.MethodPost, "/notes", "notes.form", ""))
	if !slices.Contains(*log, "notes.process") {
		t.Errorf("POST notes.form to /notes: no notes.process in %v", *log)
	}
}

func TestGateGuardCoversPagesBelow(t *testing.T) {
	m, log := site(t, func(r map[string]*fakeRoute) {
		r["/notes"].fail = map[string]error{"guard": NotFound()}
	})
	if w := serve(m, formRequest(t, http.MethodPost, "/notes/7", "item.form", "")); w.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", w.Code)
	}
	wantLog(t, log, "root.guard notes.guard")
}

func TestGateFormIsNotOnThePageBelow(t *testing.T) {
	m, log := site(t, nil)
	w := serve(m, formRequest(t, http.MethodPost, "/notes/7", "notes.form", ""))
	if w.Code != http.StatusBadRequest || w.Body.String() != "This form is not on this page." {
		t.Errorf("got %d %q, want 400 This form is not on this page.", w.Code, w.Body.String())
	}
	if slices.Contains(*log, "notes.init") || slices.Contains(*log, "notes.process") {
		t.Errorf("the gate's form hooks ran on the page below it: %v", *log)
	}
}

func TestRootGate(t *testing.T) {
	m, _ := site(t, func(r map[string]*fakeRoute) { r["/"].layout = false })
	wantPage(t, doGet(t, m, "/"), "<root></root>")

	m, log := site(t, func(r map[string]*fakeRoute) { r["/"].layout = false })
	wantPage(t, doGet(t, m, "/home"), "<home></home>")
	wantLog(t, log, "root.guard home.guard home.init home.data")
}

func TestLayoutBelowGate(t *testing.T) {
	m, log := site(t, func(r map[string]*fakeRoute) {
		r["/a"] = &fakeRoute{name: "a"}
		r["/a/b"] = &fakeRoute{name: "b", layout: true}
		r["/a/b/_id_"] = &fakeRoute{name: "leaf"}
	})
	wantPage(t, doGet(t, m, "/a/b/5"), "<root><b><leaf></leaf></b></root>")
	wantLog(t, log, "root.guard a.guard b.guard leaf.guard root.init b.init leaf.init root.data b.data leaf.data")
}

func TestParameterFolderGate(t *testing.T) {
	var org *fakeRoute
	m, _ := site(t, func(r map[string]*fakeRoute) {
		org = &fakeRoute{name: "org"}
		r["/_org_"] = org
		r["/_org_/panel"] = &fakeRoute{name: "orgpanel"}
	})
	wantPage(t, doGet(t, m, "/acme"), "<root><org></org></root>")

	org.params = nil
	wantPage(t, doGet(t, m, "/acme/panel"), "<root><orgpanel></orgpanel></root>")
	if got := org.params["org"]; got != "acme" {
		t.Errorf("org.guard saw URLParam(org) = %q, want acme", got)
	}
}

func TestLayoutWithoutDefaultIsNotFound(t *testing.T) {
	for _, def := range []string{"", ".", "/"} {
		m, _ := site(t, func(r map[string]*fakeRoute) { r["/admin"].def = def })
		if w := doGet(t, m, "/admin"); w.Code != http.StatusNotFound || w.Header().Get("Location") != "" {
			t.Errorf("default %q: got %d Location %q, want 404 and no Location", def, w.Code, w.Header().Get("Location"))
		}
	}
}

func TestLayoutRedirectStaysOnSite(t *testing.T) {
	m, _ := site(t, func(r map[string]*fakeRoute) {
		r["/_org_"] = &fakeRoute{name: "org", layout: true, def: "panel"}
		r["/_org_/panel"] = &fakeRoute{name: "orgpanel"}
	})
	w := doGet(t, m, "/%5Cevil.example")
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/%5Cevil.example/panel" {
		t.Errorf("got %d Location %q, want 302 /%%5Cevil.example/panel", w.Code, w.Header().Get("Location"))
	}
}

func TestLayoutWithNothingBelowRendersItself(t *testing.T) {
	m, log := site(t, func(r map[string]*fakeRoute) { r["/home"].layout = true })
	wantPage(t, doGet(t, m, "/home"), "<root><home></home></root>")
	wantLog(t, log, "root.guard home.guard root.init home.init root.data home.data")
}

func TestParamFolderNameIsAValue(t *testing.T) {
	var ed *fakeRoute
	m, _ := site(t, func(r map[string]*fakeRoute) { ed = r["/note/_id_/ed"] })
	wantPage(t, doGet(t, m, "/note/_id_/ed"), "<root><note><ed></ed></note></root>")
	if got := ed.params["id"]; got != "_id_" {
		t.Errorf("URLParam(id) = %q, want _id_", got)
	}

	if w := doGet(t, m, "/_id_"); w.Code != http.StatusNotFound {
		t.Errorf("GET /_id_ = %d, want 404", w.Code)
	}
}

func TestEmptySegmentIsNotFound(t *testing.T) {
	for _, target := range []string{"/note//ed", "/note//"} {
		m, log := site(t, nil)
		if w := doGet(t, m, target); w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", target, w.Code)
		}
		wantLog(t, log, "")
	}

	m, log := site(t, func(r map[string]*fakeRoute) { r["/_org_"] = &fakeRoute{name: "org"} })
	if w := doGet(t, m, "//"); w.Code != http.StatusNotFound {
		t.Errorf("GET // = %d, want 404", w.Code)
	}
	wantLog(t, log, "")
}
