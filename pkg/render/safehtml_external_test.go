package render_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sergei-svistunov/go-ssr/pkg/render"
)

type card struct{ Body render.SafeHTML }

// A type of another package that holds a SafeHTML refuses decoding too.
func TestSafeHTMLRefusesDecodingInAnotherPackage(t *testing.T) {
	var c card
	if err := json.Unmarshal([]byte(`{"Body":"<img src=x onerror=alert(1)>"}`), &c); err == nil || !strings.Contains(err.Error(), "markup cannot be decoded from input") {
		t.Errorf("got %v, want a refusal", err)
	}
}
