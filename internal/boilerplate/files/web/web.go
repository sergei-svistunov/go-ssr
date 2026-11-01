package web

import (
	"net/http"

	ssrMux "github.com/sergei-svistunov/go-ssr/pkg/mux"

	"<PKG_NAME>/<WEB_DIR>/pages"
)

// New returns the site's handler. csrfKey signs the CSRF tokens and is
// required: at least 32 bytes, or NewSsrHandler returns an error. Keep it in
// your secret store and give every instance that serves the site the same
// key; tokens survive a restart only if the key does. For development, any
// random key works: export CSRF_KEY=$(openssl rand -hex 32), or set CSRF_KEY
// under env in gossr.yaml for go-ssr -watch.
func New(csrfKey []byte) (http.Handler, error) {
	ssrHandler, err := pages.NewSsrHandler(ssrMux.Options{
		// The CSRF cookie is Secure-only. Uncomment for plain-http development
		// on a host other than localhost, or in Safari (which drops Secure
		// cookies on http://localhost too); never enable in production.
		// InsecureCookies: true,

		CSRFKey: csrfKey,
	})
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("/", ssrHandler)

	return mux, nil
}
