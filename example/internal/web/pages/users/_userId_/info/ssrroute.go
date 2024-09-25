package info

import (
	"context"
	"io"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

type RouteData struct {
	User struct {
		Age  uint8
		Info string
	}
}

type RouteDataProvider interface {
	GetRouteUsers_userId_InfoData(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error
}

type Route[DataProvider RouteDataProvider] struct{}

func (Route[DataProvider]) GetDataContext(ctx context.Context, r *mux.Request, w mux.ResponseWriter, dp DataProvider, child mux.DataContext) (mux.DataContext, error) {
	var (
		dataCtx = &dataContext{RouteDataContext: mux.RouteDataContext{
			Child: child,
			Assets: []string{
				"<script defer=\"defer\" src=\"/static/js/pages/users/_userId_/info.a2435ab41912ef57ab27.js\"></script>",
			},
		}}
	)
	if err := dp.GetRouteUsers_userId_InfoData(ctx, r, w, &dataCtx.RouteData); err != nil {
		return nil, err
	}
	return dataCtx, nil
}

func (Route[DataProvider]) GetDefaultSubRoute(ctx context.Context, r *mux.Request, dp DataProvider) (string, error) {
	return "", nil
}

type dataContext struct {
	mux.RouteDataContext
	RouteData
}

func (c *dataContext) Write(w io.Writer) error {
	user := c.RouteData.User
	if _, err := w.Write(_3f4nhuoit2tk0h0mfhpasq5pofsd7gr4ok31ucstutmmg7eo6v90); err != nil {
		return err
	}
	if user.Age <= 18 {
		if _, err := w.Write(_vpdk5heaer24etml7ge9711a0mvj4ahrr8p51418dielc0v9la4g); err != nil {
			return err
		}
	} else if user.Age <= 30 {
		if _, err := w.Write(_g7sv295h764ok5m5537ahc6f232nq98bloviqv836491mb9h19p0); err != nil {
			return err
		}
	} else if user.Age <= 60 {
		if _, err := w.Write(_op9ioepv8l07155uf33djfn67q21vmlv19susgefh9vnirjmd78g); err != nil {
			return err
		}
	} else {
		if _, err := w.Write(_l31gs7l9bqjbb89garbu034qb1qhulh2obnf5g8mvdeobdousrhg); err != nil {
			return err
		}
	}
	if _, err := w.Write(_iqb8ohfm40348bk2ahvdpntkn5dmfdd2hbk997vdalt0t3lon340); err != nil {
		return err
	}
	if _, err := mux.WriteHtmlEscaped(w, user.Info); err != nil {
		return err
	}
	if _, err := w.Write(_t0tjaoi816bk6ui6d7pdoti4rb29dleh9ivqoe0pis4t493t7ke0); err != nil {
		return err
	}
	return nil
}

var (
	_3f4nhuoit2tk0h0mfhpasq5pofsd7gr4ok31ucstutmmg7eo6v90 = []byte{
		0x0a, 0x0a, 0x0a, 0x3c, 0x68, 0x31, 0x3e, 0x41, 0x67, 0x65, 0x20, 0x67, 0x72, 0x6f, 0x75, 0x70, 0x3a, 0x0a, 0x20, 0x20, 0x20, 0x20,
	}
	_g7sv295h764ok5m5537ahc6f232nq98bloviqv836491mb9h19p0 = []byte{
		0x3c, 0x73, 0x70, 0x61, 0x6e, 0x3e, 0x31, 0x39, 0x2d, 0x33, 0x30, 0x3c, 0x2f, 0x73, 0x70, 0x61, 0x6e, 0x3e,
	}
	_iqb8ohfm40348bk2ahvdpntkn5dmfdd2hbk997vdalt0t3lon340 = []byte{
		0x0a, 0x3c, 0x2f, 0x68, 0x31, 0x3e, 0x0a, 0x0a, 0x3c, 0x70, 0x20, 0x63, 0x6c, 0x61, 0x73, 0x73, 0x3d, 0x22, 0x6d, 0x74, 0x2d, 0x33, 0x22, 0x3e,
	}
	_l31gs7l9bqjbb89garbu034qb1qhulh2obnf5g8mvdeobdousrhg = []byte{
		0x3c, 0x73, 0x70, 0x61, 0x6e, 0x3e, 0x36, 0x30, 0x2b, 0x3c, 0x2f, 0x73, 0x70, 0x61, 0x6e, 0x3e,
	}
	_op9ioepv8l07155uf33djfn67q21vmlv19susgefh9vnirjmd78g = []byte{
		0x3c, 0x73, 0x70, 0x61, 0x6e, 0x3e, 0x33, 0x31, 0x2d, 0x36, 0x30, 0x3c, 0x2f, 0x73, 0x70, 0x61, 0x6e, 0x3e,
	}
	_t0tjaoi816bk6ui6d7pdoti4rb29dleh9ivqoe0pis4t493t7ke0 = []byte{
		0x3c, 0x2f, 0x70, 0x3e, 0x0a,
	}
	_vpdk5heaer24etml7ge9711a0mvj4ahrr8p51418dielc0v9la4g = []byte{
		0x3c, 0x73, 0x70, 0x61, 0x6e, 0x3e, 0x30, 0x2d, 0x31, 0x38, 0x3c, 0x2f, 0x73, 0x70, 0x61, 0x6e, 0x3e,
	}
)
