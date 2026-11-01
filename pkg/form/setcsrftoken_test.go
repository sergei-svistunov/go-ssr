package form

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

// captureRoute records the Request its Guard was handed and stops there.
type captureRoute struct{ got **mux.Request }

func (c captureRoute) NewState() mux.RouteState { return &captureState{got: c.got} }

func (captureRoute) Layout() bool { return false }

type captureState struct {
	mux.Frame
	got **mux.Request
}

func (s *captureState) Guard(_ context.Context, r *mux.Request) error {
	*s.got = r
	return mux.NewHttpError(http.StatusTeapot, "captured")
}

func (*captureState) InitForms(context.Context, *mux.Request, mux.ResponseWriter) error { return nil }

func (*captureState) SubmitForm(context.Context, *mux.Request, mux.ResponseWriter, string) (bool, error) {
	return false, nil
}

func (*captureState) Data(context.Context, *mux.Request, mux.ResponseWriter) error { return nil }

func (*captureState) DefaultRoute(context.Context, *mux.Request) (string, error) { return "", nil }

func (*captureState) Write(io.Writer) error { return nil }

func TestSetCSRFToken_FillsEveryForm(t *testing.T) {
	var r *mux.Request
	m, err := mux.New(map[string]mux.Route{"/": captureRoute{got: &r}},
		mux.Options{CSRFKey: []byte("test-key-test-key-test-key-test1")})
	if err != nil {
		t.Fatal(err)
	}
	m.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	w := httptest.NewRecorder()
	var leaf, layout BaseFormValues
	if err := SetCSRFToken(r, w, &leaf, &layout); err != nil {
		t.Fatal(err)
	}
	c, err := http.ParseSetCookie(w.Header().Get("Set-Cookie"))
	if err != nil {
		t.Fatal(err)
	}
	if leaf.GetCSRFToken() == "" || leaf.GetCSRFToken() != c.Value || layout.GetCSRFToken() != c.Value {
		t.Errorf("forms carry %q and %q, want the cookie's token %q", leaf.GetCSRFToken(), layout.GetCSRFToken(), c.Value)
	}
}

func TestSetCSRFToken_FailsWithoutAMux(t *testing.T) {
	var f BaseFormValues
	r := mux.NewRequest(httptest.NewRequest(http.MethodGet, "/", nil))
	if err := SetCSRFToken(r, httptest.NewRecorder(), &f); err == nil || f.GetCSRFToken() != "" {
		t.Errorf("SetCSRFToken without a mux: err %v, token %q; want an error and no token", err, f.GetCSRFToken())
	}
}
