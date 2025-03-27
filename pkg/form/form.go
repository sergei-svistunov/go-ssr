package form

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var MaxMultipartMemory int64 = 32 << 20 // 32 MB
const CSRFTokenName = "_csrf_token_"

type BaseFormValues struct {
	csrfToken string
	error     string
	validated bool
	elements  []Element
}

type Element interface {
	HasError() bool
}

func (f *BaseFormValues) SetElements(elements []Element) { f.elements = elements }
func (f *BaseFormValues) MarkValidated()                 { f.validated = true }
func (f *BaseFormValues) IsValidated() bool              { return f.validated }
func (f *BaseFormValues) GetCSRFToken() string           { return f.csrfToken }
func (f *BaseFormValues) SetError(err string)            { f.error = err }
func (f *BaseFormValues) GetError() string               { return f.error }

func (f *BaseFormValues) HasError() bool {
	if f.error != "" {
		return true
	}

	for _, e := range f.elements {
		if e.HasError() {
			return true
		}
	}

	return false
}

func IsMultipart(r *mux.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), MultipartFormData)
}

func SetCSRFToken(r *mux.Request, w mux.ResponseWriter, forms ...*BaseFormValues) error {
	var csrfToken string
	if r.Method == http.MethodPost {
		if c, err := r.Cookie(CSRFTokenName); err == nil {
			csrfToken = c.Value
		} else {
			return mux.NewHttpError(http.StatusBadRequest, "Missed CSRF token")
		}
	} else {
		token := make([]byte, 32)
		if _, err := rand.Read(token); err != nil {
			return err
		}
		csrfToken = base64.StdEncoding.EncodeToString(token)
	}

	csrfCookie := http.Cookie{
		Name:     CSRFTokenName,
		Value:    csrfToken,
		HttpOnly: true,
		Expires:  time.Now().Add(24 * time.Hour),
	}
	w.Header().Add("Set-Cookie", csrfCookie.String())
	for _, form := range forms {
		form.csrfToken = csrfToken
	}

	return nil
}

func Parse(r *mux.Request) (string, error) {
	var csrfToken string
	if IsMultipart(r) {
		if err := r.ParseMultipartForm(MaxMultipartMemory); err != nil {
			return "", mux.NewHttpError(http.StatusBadRequest, err.Error())
		}
		if vs := r.MultipartForm.Value[CSRFTokenName]; len(vs) > 0 {
			csrfToken = vs[0]
		}
	} else {
		if err := r.ParseForm(); err != nil {
			return "", mux.NewHttpError(http.StatusBadRequest, err.Error())
		}
		if vs := r.PostForm[CSRFTokenName]; len(vs) > 0 {
			csrfToken = vs[0]
		}
	}

	tokenParts := strings.SplitN(csrfToken, ":", 2)
	csrfCookie, _ := r.Cookie(CSRFTokenName)
	if len(tokenParts) != 2 || len(tokenParts[1]) == 0 || csrfCookie == nil || tokenParts[1] != csrfCookie.Value {
		return "", mux.NewHttpError(http.StatusBadRequest, "Invalid CSRF token")
	}

	return tokenParts[0], nil
}
