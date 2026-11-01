package mux

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// cspDirective is a directive of the Content-Security-Policy with its
// default sources. One without defaults is sent only when Options.CSPSources
// gives it some; a fixed one takes none.
type cspDirective struct {
	name     string
	defaults []string
	fixed    bool
}

// cspDirectives lists the directives in the order they are sent.
var cspDirectives = []cspDirective{
	{name: "default-src", defaults: []string{"'self'"}},
	{name: "script-src", defaults: []string{"'self'"}},
	{name: "style-src", defaults: []string{"'self'"}},
	{name: "img-src", defaults: []string{"'self'", "data:"}},
	{name: "font-src", defaults: []string{"'self'"}},
	{name: "connect-src", defaults: []string{"'self'"}},
	{name: "object-src", defaults: []string{"'none'"}, fixed: true},
	{name: "base-uri", defaults: []string{"'none'"}, fixed: true},
	{name: "frame-ancestors", defaults: []string{"'none'"}, fixed: true},
	{name: "form-action", defaults: []string{"'self'"}},
	{name: "media-src"},
	{name: "worker-src"},
	{name: "frame-src"},
	{name: "manifest-src"},
}

// cspPolicy is a Content-Security-Policy split where the request's
// WebSocket origin goes: after connect-src's defaults.
type cspPolicy struct {
	head, tail string
}

// defaultCSP is the policy of a request that did not pass through a Mux.
var defaultCSP = func() *cspPolicy {
	p, err := newCSP(nil)
	if err != nil {
		panic(err)
	}
	return p
}()

// newCSP builds the policy with sources added to the defaults, refusing a
// directive that takes none and a source that would widen the policy beyond
// a list of origins or break out of its directive.
func newCSP(sources map[string][]string) (*cspPolicy, error) {
	for name, list := range sources {
		i := slices.IndexFunc(cspDirectives, func(d cspDirective) bool { return d.name == name })
		if i < 0 || cspDirectives[i].fixed {
			return nil, fmt.Errorf("mux: CSPSources: directive %q takes no added sources", name)
		}
		for _, s := range list {
			if err := checkCSPSource(name, s); err != nil {
				return nil, fmt.Errorf("mux: CSPSources: %s source %q %w", name, s, err)
			}
		}
	}

	p := &cspPolicy{}
	var b strings.Builder
	for i, d := range cspDirectives {
		list := append(slices.Clone(d.defaults), sources[d.name]...)
		if len(list) == 0 {
			continue
		}
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(d.name)
		for j, s := range list {
			b.WriteString(" ")
			b.WriteString(s)
			if d.name == "connect-src" && j == len(d.defaults)-1 {
				p.head = b.String()
				b.Reset()
			}
		}
	}
	p.tail = b.String()
	return p, nil
}

var (
	reCSPScheme     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
	reCSPSchemeOnly = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:$`)
)

// checkCSPSource refuses a source that breaks out of its directive or lifts
// the protection the directive gives: any origin, an unsafe- keyword,
// 'strict-dynamic', and in script-src a whole scheme.
func checkCSPSource(directive, s string) error {
	lower := strings.ToLower(s)
	switch {
	case s == "":
		return errors.New("is empty")
	case strings.ContainsFunc(s, func(r rune) bool {
		return r == ';' || r == ',' || unicode.IsSpace(r) || unicode.IsControl(r)
	}):
		return errors.New("contains whitespace, ';', ',' or a control character")
	case strings.HasPrefix(lower, "'") && strings.Contains(lower, "unsafe-"),
		lower == "'strict-dynamic'":
		return errors.New("lifts the policy's protection")
	case cspSourceHost(s) == "*":
		return errors.New("allows every host")
	case directive == "script-src" && reCSPSchemeOnly.MatchString(s):
		return errors.New("allows scripts from a whole scheme")
	}
	return nil
}

// cspSourceHost returns the host part of a host source: what follows an
// optional scheme and "//", up to a port or path.
func cspSourceHost(s string) string {
	if loc := reCSPScheme.FindStringIndex(s); loc != nil {
		rest, ok := strings.CutPrefix(s[loc[1]:], "//")
		if !ok {
			return ""
		}
		s = rest
	}
	host, _, _ := strings.Cut(s, "/")
	host, _, _ = strings.Cut(host, ":")
	return host
}

var reCSPHost = regexp.MustCompile(`^([A-Za-z0-9-]+\.)*[A-Za-z0-9-]+(:[0-9]{1,5})?$`)
var reCSPIPv6Host = regexp.MustCompile(`^\[([0-9A-Fa-f:.]+)\](:[0-9]{1,5})?$`)

// cspHost reports whether host is safe to write into the policy: a domain
// name or a bracketed IPv6 address, each with an optional port.
func cspHost(host string) bool {
	if reCSPHost.MatchString(host) {
		return true
	}
	m := reCSPIPv6Host.FindStringSubmatch(host)
	if m == nil {
		return false
	}
	addr, err := netip.ParseAddr(m[1])
	return err == nil && addr.Is6()
}

// setHeaders sets the security headers of a response to r, replacing any
// already set. The policy's connect-src admits the WebSocket origin of r's
// Host, which is left out when Host is not a plain host name.
func setHeaders(h http.Header, r *http.Request) {
	p := defaultCSP
	if o, ok := r.Context().Value(requestOptionsKey{}).(requestOptions); ok && o.csp != nil {
		p = o.csp
	}
	csp := p.head + p.tail
	if cspHost(r.Host) {
		scheme := "ws://"
		if SecureRequest(r) {
			scheme = "wss://"
		}
		csp = p.head + " " + scheme + r.Host + p.tail
	}
	h.Set("Content-Security-Policy", csp)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
}
