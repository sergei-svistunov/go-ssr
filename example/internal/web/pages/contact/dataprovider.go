package contact

import (
	"context"
	"net/http"
	"strings"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/pkg/form"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var _ RouteDataProvider = &DP{}

type DP struct{}

func NewDP(_ *model.Model) *DP {
	return &DP{}
}

// Data is called on every page load. Shows success banner when ?sent=1 is present.
func (p *DP) Data(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	data.Success = r.URL.Query().Get("sent") == "1"
	return nil
}

// InitContact populates the Topic select options.
func (p *DP) InitContact(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *FormContactValues) error {
	data.Topic.SetOptions([]form.SelectOptionElement[string]{
		form.SelectOption[string]{Value: "general", Label: "General Enquiry"},
		form.SelectOption[string]{Value: "support", Label: "Technical Support"},
		form.SelectOption[string]{Value: "billing", Label: "Billing"},
		form.SelectOption[string]{Value: "feedback", Label: "Feedback"},
	})
	return nil
}

// ProcessContact validates the submitted contact form.
// On success it redirects to /contact?sent=1 to show the success banner.
// On validation failure it leaves form errors in place so the template renders them.
func (p *DP) ProcessContact(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *FormContactValues) error {
	name := strings.TrimSpace(data.Name.GetValue())
	email := strings.TrimSpace(data.Email.GetValue())
	message := strings.TrimSpace(data.Message.GetValue())

	if name == "" {
		data.Name.SetError("Name is required")
	}
	if email == "" {
		data.Email.SetError("Email is required")
	} else if !strings.Contains(email, "@") {
		data.Email.SetError("Please enter a valid email address")
	}
	if message == "" {
		data.Message.SetError("Message is required")
	}

	if data.HasError() {
		return nil
	}

	// Success: redirect to the same page with ?sent=1 so the success banner shows.
	return mux.Redirect(http.StatusFound, "/contact?sent=1")
}
