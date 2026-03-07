package add

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"path"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/pkg/form"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var _ RouteDataProvider = &DPUsersAdd{}

type FormValue struct {
	Value string
	Error string
	Class string
}

type DPUsersAdd struct {
	model *model.Model
}

func (p *DPUsersAdd) InitRouteUsersAddData_FormAdd(ctx context.Context, r *mux.Request, w mux.ResponseWriter, formData *FormAddValues) error {
	formData.Name.SetValue("John Doe")

	formData.Select.SetOptions([]form.SelectOptionElement[uint8]{
		form.SelectOptionGroup[uint8]{
			Label: "Group 1",
			Options: []form.SelectOptionElement[uint8]{
				form.SelectOption[uint8]{Value: 1, Label: "Value 1"},
				form.SelectOption[uint8]{Value: 2, Label: "Value 2"},
				form.SelectOption[uint8]{Value: 3, Label: "Value 3", Disabled: true},
			},
		},
		form.SelectOptionGroup[uint8]{
			Label: "Group 2",
			Options: []form.SelectOptionElement[uint8]{
				form.SelectOption[uint8]{Value: 4, Label: "Value 4"},
				form.SelectOption[uint8]{Value: 5, Label: "Value 5"},
				form.SelectOption[uint8]{Value: 6, Label: "Value 6"},
			},
		},
	})

	formData.MultSelect.SetOptions(formData.Select.GetOptions())

	return nil
}

func (p *DPUsersAdd) ProcessRouteUsersAddData_FormAdd(ctx context.Context, r *mux.Request, w mux.ResponseWriter, form *FormAddValues) error {
	log.Printf("%#v\n", form)

	f, err := form.Image.GetValue().Open()
	if err != nil {
		return err
	}
	defer f.Close()
	io.ReadAll(f)

	if err := p.model.AddUser(model.MockUser{
		Id:       0,
		Login:    form.Login.GetValue(),
		Name:     form.Name.GetValue(),
		Age:      form.Age.GetValue(),
		ImageUrl: "",
		Phones:   nil,
		Emails:   nil,
		Info:     form.Description.GetValue(),
	}); err != nil {
		if errors.Is(err, model.ErrInvalidLogin) {
			form.Login.SetError("Invalid characters in login")
			return nil
		} else if errors.Is(err, model.ErrUserAlreadyExists) {
			form.Login.SetError("User already exists")
			return nil
		} else if errors.Is(err, model.ErrInvalidAge) {
			form.Age.SetError("Invalid age")
			return nil
		} else {
			return err
		}
	}

	return mux.Redirect(http.StatusFound, path.Join("/users", form.Login.GetValue()))
}

func NewDP(m *model.Model) *DPUsersAdd {
	return &DPUsersAdd{m}
}

func (p *DPUsersAdd) GetRouteUsersAddData(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	if r.Method != http.MethodPost {
		return nil
	}

	if err := r.ParseForm(); err != nil {
		return mux.NewHttpError(http.StatusBadRequest, err.Error())
	}

	return nil
}
