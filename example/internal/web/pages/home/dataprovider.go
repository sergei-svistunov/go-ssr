package home

import (
	"context"
	"log"
	"math/rand"
	"time"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
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
	data.VisitorsBadgeColor = visitorsBadgeColorFor(data.VisitorsOnline)
	data.DisplayName = ""

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

// Subscribe randomly bumps the visitors online count every 4 seconds.
func (p *DP) Subscribe(ctx context.Context, r *mux.Request, state *ReactiveState) error {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			n := 10 + rand.Intn(491) // 10–500
			p.m.SetVisitorsOnline(n)
			state.SetVisitorsOnline(n)
			state.SetVisitorsBadgeColor(visitorsBadgeColorFor(n))
		}
	}
}

// visitorsBadgeColorFor maps the visitor count to a CSS colour. Used in the
// reactive style="color: {{ visitorsBadgeColor }}" binding on the home page.
func visitorsBadgeColorFor(n int) string {
	switch {
	case n < 50:
		return "#dc3545" // red
	case n < 200:
		return "#fd7e14" // orange
	default:
		return "#198754" // green
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
