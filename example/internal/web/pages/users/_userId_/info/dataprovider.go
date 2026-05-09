package info

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/example/internal/web/ctxdata"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var _ RouteDataProvider = &DP{}

type DP struct {
	model *model.Model
}

func NewDP(d *model.Model) *DP {
	return &DP{model: d}
}

func (p *DP) Data(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	ctxdata.FromContext(ctx).PageTitle += " | User info"

	dbUser := p.model.GetUserByLogin(r.URLParam("userId"))
	if dbUser == nil {
		return mux.NewHttpError(http.StatusNotFound, "user wasn't found")
	}

	data.User.Age = dbUser.Age
	data.User.Info = dbUser.Info

	login := r.URLParam("userId")
	ts := p.model.UserLastSeen(login)
	if ts == 0 {
		ts = time.Now().Unix()
		p.model.TouchUserLastSeen(login, ts)
	}
	data.LastSeen = formatLastSeen(ts)

	return nil
}

// Subscribe updates lastSeen every 30 seconds.
func (p *DP) Subscribe(ctx context.Context, r *mux.Request, state *ReactiveState) error {
	login := r.URLParam("userId")
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			ts := p.model.UserLastSeen(login)
			if ts == 0 {
				ts = time.Now().Unix()
			}
			state.SetLastSeen(formatLastSeen(ts))
		}
	}
}

func formatLastSeen(ts int64) string {
	elapsed := time.Since(time.Unix(ts, 0))
	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < 2*time.Minute:
		return "1 minute ago"
	default:
		return fmt.Sprintf("%d minutes ago", int(elapsed.Minutes()))
	}
}
