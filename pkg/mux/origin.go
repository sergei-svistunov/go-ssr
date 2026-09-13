package mux

import (
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// CrossSite reports whether the browser says r was not initiated by this
// origin, and the origin is not listed in originPatterns either. It is the
// one origin rule shared by form submissions and reactive WebSocket
// handshakes.
//
// Sec-Fetch-Site and Origin are set by the browser and cannot be forged from
// a page. "same-origin" (or "none", a user-typed navigation) is trusted
// outright, which keeps requests working behind a proxy that rewrites Host.
// "same-site" and "cross-site" are rejected unless Origin matches a pattern:
// a sibling subdomain is exactly where a planted cookie would come from.
// Without Sec-Fetch-Site the Origin host is compared to the request Host,
// ignoring the port the Origin's scheme implies, and a request that arrived
// over TLS (directly, or behind a proxy sending X-Forwarded-Proto: https)
// only accepts an https Origin. A request carrying neither header (a
// non-browser client), or "Origin: null" as Referrer-Policy: no-referrer
// produces on a same-origin POST, is no signal either way and passes;
// callers keep their own second line of defence (the CSRF token, the
// session).
//
// Patterns follow websocket.AcceptOptions: each is path.Match'ed, case-
// insensitively, against the origin's host[:port] as the browser sends it
// (default ports omitted), or against scheme://host[:port] when the pattern
// contains "://". "dev.example" matches https://dev.example, "dev.example:*"
// any explicit port, "https://dev.example" also pins the scheme.
func CrossSite(r *http.Request, originPatterns []string) bool {
	return crossSite(r, originPatterns, false)
}

// CrossSiteStrict is CrossSite for a request whose only defence is this
// check: a WebSocket handshake, which carries no CSRF token behind it. It
// differs in one way — an opaque "Origin: null" is refused instead of being
// treated as no signal. A browser always sends a real Origin on a handshake,
// and null is what a sandboxed iframe or a data: document sends; the
// tolerance in CrossSite exists for a same-origin form POST under
// Referrer-Policy: no-referrer, which has no handshake equivalent. An absent
// Origin still passes: that is a non-browser client.
func CrossSiteStrict(r *http.Request, originPatterns []string) bool {
	return crossSite(r, originPatterns, true)
}

func crossSite(r *http.Request, originPatterns []string, refuseOpaqueOrigin bool) bool {
	origin := r.Header.Get("Origin")
	if origin == "null" {
		if refuseOpaqueOrigin {
			return true
		}
		origin = ""
	}
	u, err := url.Parse(origin)
	if err != nil || (origin != "" && u.Host == "") {
		return true
	}

	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return false
	case "same-site", "cross-site":
		return origin == "" || !originListed(u, originPatterns)
	}
	if origin == "" {
		return false
	}
	// Checked first, so an origin the operator listed on purpose is honoured
	// here exactly as it is in the Sec-Fetch-Site branch above.
	if originListed(u, originPatterns) {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	// A page that reached a TLS endpoint was itself served over https.
	if SecureRequest(r) && scheme != "https" {
		return true
	}
	return !strings.EqualFold(stripDefaultPort(u.Host, scheme), stripDefaultPort(r.Host, scheme))
}

// SecureRequest reports whether r arrived over TLS, directly or through a
// proxy that says so. A client forging X-Forwarded-Proto only makes the
// origin check stricter and silences a diagnostic; it grants nothing.
func SecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	// Chained proxies append to the header, so the browser's own scheme is
	// the first element of "https, http".
	proto, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Proto"), ",")
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}

// CrossSite is CrossSite(r.Request, r.OriginPatterns).
func (r *Request) CrossSite() bool {
	return CrossSite(r.Request, r.OriginPatterns)
}

// originListed matches u against patterns. A pattern path.Match cannot parse
// counts as no match: New panics on one at construction, but a request path
// must not — CrossSite is exported and callers pass their own patterns, and
// a panic there would turn bad configuration into a downed request.
func originListed(u *url.URL, patterns []string) bool {
	for _, pattern := range patterns {
		target := u.Host
		if strings.Contains(pattern, "://") {
			target = u.Scheme + "://" + u.Host
		}
		if ok, err := path.Match(strings.ToLower(pattern), strings.ToLower(target)); err == nil && ok {
			return true
		}
	}
	return false
}

// stripDefaultPort removes the port the scheme implies, so an Origin, which
// never carries a default port, compares equal to a Host that does.
func stripDefaultPort(host, scheme string) string {
	switch scheme {
	case "https":
		return strings.TrimSuffix(host, ":443")
	case "http":
		return strings.TrimSuffix(host, ":80")
	}
	return host
}

// validateOriginPatterns panics on a pattern that cannot work, the way a bad
// regexp would at startup; a silent non-match would be chased as a proxy
// problem instead. Matching itself never panics — see originListed.
func validateOriginPatterns(patterns []string) {
	for _, p := range patterns {
		if _, err := path.Match(strings.ToLower(p), ""); err != nil {
			panic(fmt.Sprintf("mux: malformed OriginPatterns entry %q: %v", p, err))
		}
		// An IPv6 literal host starts with "[", which path.Match parses
		// happily as a character class: the pattern then never matches the
		// origin it was written for, and does match unintended hosts.
		host := p
		if i := strings.Index(p, "://"); i >= 0 {
			host = p[i+len("://"):]
		}
		if strings.HasPrefix(host, "[") {
			panic(fmt.Sprintf("mux: OriginPatterns entry %q reads as a character class, not an IPv6 literal: "+
				"escape the brackets, as in `\\[::1\\]:*`", p))
		}
	}
}
