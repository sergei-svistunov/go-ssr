package app_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/app/web/pages"
	team "example.com/app/web/pages/teams/_team_"
	members "example.com/app/web/pages/teams/_team_/members"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

func TestLayoutFlag(t *testing.T) {
	for _, tc := range []struct {
		path  string
		route mux.Route
		want  bool
	}{
		{"/", pages.NewRoute(pages.NewDP()), true},
		{"/teams/_team_", team.NewRoute(team.NewDP()), false},
		{"/teams/_team_/members", members.NewRoute(members.NewDP()), false},
	} {
		if got := tc.route.Layout(); got != tc.want {
			t.Errorf("%s: Layout() = %t, want %t", tc.path, got, tc.want)
		}
	}
}

func TestParentPage(t *testing.T) {
	h, err := pages.NewSsrHandler(mux.Options{CSRFKey: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	get := func(target string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		return w
	}

	w := get("/teams/red")
	if body := w.Body.String(); w.Code != http.StatusOK || !strings.Contains(body, "<header>site</header>") || !strings.Contains(body, `class="team"`) {
		t.Errorf("GET /teams/red = %d %q, want 200 with the team page inside the root layout", w.Code, body)
	}

	team.Guarded = ""
	w = get("/teams/red/members")
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, "<header>site</header>") || !strings.Contains(body, `class="members"`) {
		t.Errorf("GET /teams/red/members = %d %q, want 200 with the members page inside the root layout", w.Code, body)
	}
	if strings.Contains(body, `class="team"`) {
		t.Errorf("GET /teams/red/members shows the team page: %q", body)
	}
	if team.Guarded != "red" {
		t.Errorf("the team page's Guard saw team %q, want red", team.Guarded)
	}

	if w := get("/teams"); w.Code != http.StatusNotFound {
		t.Errorf("GET /teams = %d, want 404", w.Code)
	}
}
