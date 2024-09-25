package contacts

import (
	"context"
	"io"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

type RouteData struct {
	Emails []string
	Phones []string
}

type RouteDataProvider interface {
	GetRouteUsers_userId_ContactsData(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error
}

type Route[DataProvider RouteDataProvider] struct{}

func (Route[DataProvider]) GetDataContext(ctx context.Context, r *mux.Request, w mux.ResponseWriter, dp DataProvider, child mux.DataContext) (mux.DataContext, error) {
	var (
		dataCtx = &dataContext{RouteDataContext: mux.RouteDataContext{
			Child: child,
			Assets: []string{
				"<link href=\"/static/css/pages/users/_userId_/contacts.ebaa73568a9350e8216c.css\" rel=\"stylesheet\">",
				"<script defer=\"defer\" src=\"/static/js/pages/users/_userId_/contacts.ebaa73568a9350e8216c.js\"></script>",
			},
		}}
	)
	if err := dp.GetRouteUsers_userId_ContactsData(ctx, r, w, &dataCtx.RouteData); err != nil {
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
	emails := c.RouteData.Emails
	phones := c.RouteData.Phones
	if _, err := w.Write(_bg4b54897en5470jp8urn1i3lfhu5adv362a24sfftrdqiposv00); err != nil {
		return err
	}
	for _, phone := range phones {
		if _, err := w.Write(_u7gqvvem62261dt1j9p6b7ql4n71iv9vds5b66o9fnqe1vv1uf3g); err != nil {
			return err
		}
		if _, err := mux.WriteHtmlEscaped(w, phone); err != nil {
			return err
		}
		if _, err := w.Write(_2r9972n9hj841mpqco77bjcufn7gtu387u4ppfrdmdeb5tgjm380); err != nil {
			return err
		}
	}
	if _, err := w.Write(_8fpq3m153g26en70f7jsal5uose9iioj6p9k66bvhh3rb9holb00); err != nil {
		return err
	}
	for _, email := range emails {
		if _, err := w.Write(_u7gqvvem62261dt1j9p6b7ql4n71iv9vds5b66o9fnqe1vv1uf3g); err != nil {
			return err
		}
		if _, err := mux.WriteHtmlEscaped(w, email); err != nil {
			return err
		}
		if _, err := w.Write(_2r9972n9hj841mpqco77bjcufn7gtu387u4ppfrdmdeb5tgjm380); err != nil {
			return err
		}
	}
	if _, err := w.Write(_v3722p13l2f4l4sghjrpqh64tabga6vgd6s2ldi1edj9djk9sohg); err != nil {
		return err
	}
	return nil
}

var (
	_2r9972n9hj841mpqco77bjcufn7gtu387u4ppfrdmdeb5tgjm380 = []byte{
		0x3c, 0x2f, 0x6c, 0x69, 0x3e,
	}
	_8fpq3m153g26en70f7jsal5uose9iioj6p9k66bvhh3rb9holb00 = []byte{
		0x0a, 0x20, 0x20, 0x20, 0x20, 0x3c, 0x2f, 0x75, 0x6c, 0x3e, 0x0a, 0x0a, 0x20, 0x20, 0x20, 0x20, 0x3c, 0x68, 0x33, 0x3e, 0x45, 0x6d, 0x61, 0x69,
		0x6c, 0x73, 0x3c, 0x2f, 0x68, 0x33, 0x3e, 0x0a, 0x20, 0x20, 0x20, 0x20, 0x3c, 0x75, 0x6c, 0x3e, 0x0a, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
		0x20,
	}
	_bg4b54897en5470jp8urn1i3lfhu5adv362a24sfftrdqiposv00 = []byte{
		0x0a, 0x0a, 0x0a, 0x3c, 0x64, 0x69, 0x76, 0x3e, 0x0a, 0x20, 0x20, 0x20, 0x20, 0x3c, 0x68, 0x33, 0x3e, 0x50, 0x68, 0x6f, 0x6e, 0x65, 0x73, 0x3c,
		0x2f, 0x68, 0x33, 0x3e, 0x0a, 0x20, 0x20, 0x20, 0x20, 0x3c, 0x75, 0x6c, 0x3e, 0x0a, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
	}
	_u7gqvvem62261dt1j9p6b7ql4n71iv9vds5b66o9fnqe1vv1uf3g = []byte{
		0x3c, 0x6c, 0x69, 0x3e,
	}
	_v3722p13l2f4l4sghjrpqh64tabga6vgd6s2ldi1edj9djk9sohg = []byte{
		0x0a, 0x20, 0x20, 0x20, 0x20, 0x3c, 0x2f, 0x75, 0x6c, 0x3e, 0x0a, 0x3c, 0x2f, 0x64, 0x69, 0x76, 0x3e,
	}
)
