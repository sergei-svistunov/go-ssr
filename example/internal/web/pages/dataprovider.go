package pages

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/example/internal/web/ctxdata"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var _ RouteDataProvider = &DP{}

type DP struct {
	m *model.Model
}

func NewDP(d *model.Model) *DP {
	return &DP{m: d}
}

func (p *DP) Data(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) > 1 {
		data.RoutePath = "/" + pathParts[1]
	}

	data.Title = func() string {
		return "Page test title<>\"" + ctxdata.FromContext(ctx).PageTitle
	}

	data.User.Image = "https://encrypted-tbn0.gstatic.com/images?q=tbn:ANd9GcTUW0u5Eiiy3oM6wcpeEE6sXCzlh8G-tX1_Iw&s"
	data.User.Name = "User<>\"Name"

	data.Balance = p.m.Balance()

	w.Header().Set("X-Custom-Header", fmt.Sprint(rand.Int63()))

	return nil
}

// Subscribe pushes live balance updates while the WebSocket is open.
// Balance increments by a small random amount every 2-3 seconds,
// simulating incoming transactions.
func (p *DP) Subscribe(ctx context.Context, r *mux.Request, state *ReactiveState) error {
	for {
		interval := time.Duration(2000+rand.Intn(1000)) * time.Millisecond
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
			delta := 1 + rand.Intn(999) // 1–999 cents
			newBalance := p.m.IncBalance(delta)
			state.SetBalance(newBalance)
		}
	}
}
