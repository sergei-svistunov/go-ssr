package home

import (
	"context"
	"log"
	"time"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
	"github.com/sergei-svistunov/go-ssr/pkg/render"
)

var _ RouteDataProvider = &DP{}

type DP struct {
	m *model.Model
}

func NewDP(d *model.Model) *DP {
	return &DP{m: d}
}

func (p *DP) DefaultRoute(ctx context.Context, r *mux.Request) (string, error) {
	return "", nil
}

func (p *DP) Data(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	data.VisitorsOnline = p.m.VisitorsOnline()
	data.VisitorsBadgeClass = visitorsBadgeClassFor(data.VisitorsOnline)
	data.DisplayName = ""
	data.Divider = render.HTMLConst(`<hr class="mt-3">`)

	// Arithmetic / field-accessor showcase values.
	data.Price = 9.99
	data.Quantity = 3.0
	data.UserAge = 25
	data.UserName = "Alice"

	// Conditional showcase.
	data.Status = "active"

	// Loop showcases.
	data.Fruits = []string{"Apple", "Banana", "Cherry"}
	data.Langs = []string{"Go", "TypeScript", "Rust"}

	return nil
}

// Subscribe moves the visitors online count to the next badge band every 4
// seconds, so the count and its badge class change on every tick.
func (p *DP) Subscribe(ctx context.Context, r *mux.Request, state *ReactiveState) error {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			n := nextVisitorsOnline(p.m.VisitorsOnline())
			p.m.SetVisitorsOnline(n)
			state.SetVisitorsOnline(n)
			state.SetVisitorsBadgeClass(visitorsBadgeClassFor(n))
		}
	}
}

// visitorsSteps holds one count per badge band, in the order the count cycles
// through them.
var visitorsSteps = []int{25, 120, 350}

// nextVisitorsOnline returns the count of the band after the one n is in.
func nextVisitorsOnline(n int) int {
	for i, s := range visitorsSteps {
		if visitorsBadgeClassFor(s) == visitorsBadgeClassFor(n) {
			return visitorsSteps[(i+1)%len(visitorsSteps)]
		}
	}
	return visitorsSteps[0]
}

// visitorsBadgeClassFor maps the visitor count to one of the badge classes
// in styles.scss. Used in the reactive class="badge {{ visitorsBadgeClass }}"
// binding on the home page.
func visitorsBadgeClassFor(n int) string {
	switch {
	case n < 50:
		return "visitors-few"
	case n < 200:
		return "visitors-some"
	default:
		return "visitors-many"
	}
}

// ValidateDisplayName accepts display names up to 50 characters and logs accepted writes.
func (p *DP) ValidateDisplayName(ctx context.Context, r *mux.Request, val string) (string, error) {
	if len(val) > 50 {
		return "", mux.NewHttpError(422, "display name must be 50 characters or fewer")
	}
	log.Printf("displayName changed to %q", val)
	return val, nil
}
