package add

import (
	"context"
	"errors"
	"net/http"
	"path"
	"strconv"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
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
	data.Login.Value = r.FormValue("login")
	data.Login.Class = "is-valid"

	data.Name.Value = r.FormValue("name")
	data.Name.Class = "is-valid"

	data.Age.Value = r.FormValue("age")
	data.Age.Class = "is-valid"

	data.Description.Value = r.FormValue("description")
	data.Description.Class = "is-valid"

	age, err := strconv.Atoi(r.FormValue("age"))
	if err != nil {
		data.Age.Error = err.Error()
	}

	if err := p.model.AddUser(model.MockUser{
		Id:       0,
		Login:    data.Login.Value,
		Name:     data.Name.Value,
		Age:      uint8(age),
		ImageUrl: "",
		Phones:   nil,
		Emails:   nil,
		Info:     data.Description.Value,
	}); err != nil {
		if errors.Is(err, model.ErrInvalidLogin) {
			data.Login.Error = "Invalid characters in login"
			data.Login.Class = "is-invalid"
		} else if errors.Is(err, model.ErrUserAlreadyExists) {
			data.Login.Error = "User already exists"
			data.Login.Class = "is-invalid"
		} else if errors.Is(err, model.ErrInvalidAge) {
			data.Age.Error = "Invalid age"
			data.Age.Class = "is-invalid"
		} else {
			return err
		}
		return nil
	}

	return mux.Redirect(http.StatusFound, path.Join("/users", data.Login.Value))
}
