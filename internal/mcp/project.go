package mcp

import (
	"bytes"
	"fmt"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sergei-svistunov/go-ssr/internal/config"
	"github.com/sergei-svistunov/go-ssr/internal/generator"
)

// project is a resolved GoSSR project plus the buffer that collects the output
// of the external tools the generator runs. On a stdio transport that output
// cannot go to stdout, and it is more useful in a tool result anyway.
type project struct {
	cfg *config.Config
	gen *generator.Generator
	out *bytes.Buffer

	webpackRan   bool
	assetsFailed bool
	assetsReason string
}

// assetPolicy decides whether a generation run builds assets first.
type assetPolicy int

const (
	assetsAuto assetPolicy = iota // build only when sources are newer than the last build
	assetsSkip
	assetsForce
)

func parseAssetPolicy(s string) (assetPolicy, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return assetsAuto, nil
	case "skip":
		return assetsSkip, nil
	case "force":
		return assetsForce, nil
	default:
		return assetsAuto, fmt.Errorf("assets must be auto, skip or force, got %q", s)
	}
}

// generate builds assets when the policy calls for it, then analyzes and
// generates. Problems with the project's content come back as diagnostics; the
// error return is for operational failures.
func (p *project) generate(policy assetPolicy) ([]generator.Diagnostic, error) {
	switch policy {
	case assetsForce:
		p.assetsReason = "requested"
		if err := p.gen.Webpack(); err != nil {
			return nil, fmt.Errorf("building assets: %w", err)
		}
		p.webpackRan = true
	case assetsAuto:
		stale, reason, err := p.gen.AssetsStale()
		if err != nil {
			return nil, err
		}
		p.assetsReason = reason
		if stale {
			if err := p.gen.Webpack(); err != nil {
				// Code generation is the job; the asset build is a convenience
				// that needs Node. Report the failure and carry on rather than
				// letting a missing toolchain block generating Go code.
				p.assetsReason = fmt.Sprintf("%s — build failed: %v", reason, err)
				p.assetsFailed = true
			} else {
				p.webpackRan = true
			}
		}
	case assetsSkip:
		if stale, reason, err := p.gen.AssetsStale(); err == nil && stale {
			p.assetsReason = reason + " (skipped on request)"
		}
	}

	diags, hardErr := splitDiagnostics(p.gen.Analyze())
	if hardErr != nil {
		return nil, hardErr
	}
	if len(diags) > 0 {
		// Generating from a partially parsed tree would write code for some
		// routes and leave stale code for the broken ones.
		return diags, nil
	}

	genDiags, hardErr := splitDiagnostics(p.gen.Generate())
	if hardErr != nil {
		return nil, hardErr
	}

	return genDiags, nil
}

// dataProviderPaths returns the set of existing dataprovider.go files, so a
// caller can report which stubs a generation run created.
func (p *project) dataProviderPaths() map[string]bool {
	found := map[string]bool{}

	_ = filepath.WalkDir(p.pagesDir(), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "static_embed" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "dataprovider.go" {
			found[path] = true
		}
		return nil
	})

	return found
}

func sortStrings(s []string) { sort.Strings(s) }

// openProject resolves the project to work on. An explicit dir wins; otherwise
// the config is found by walking up from the working directory, exactly as the
// CLI does.
func openProject(dir string, prod bool) (*project, error) {
	start := dir
	if start == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		start = wd
	}

	cfg, err := config.ReadFrom(start)
	if err != nil {
		return nil, fmt.Errorf("%w. Pass projectDir with the path to a directory inside a GoSSR project, or use gossr_init to create one", err)
	}
	cfg.Prod = prod

	p := &project{cfg: cfg, out: &bytes.Buffer{}}
	p.gen = generator.New(cfg)
	p.gen.SetOutput(nil, p.out, p.out)

	return p, nil
}

// output returns the collected tool output, trimmed to a size that is worth
// putting in front of a caller. npm in particular is voluminous, and the tail is
// where the failure is.
func (p *project) output() string {
	const limit = 8000

	s := strings.TrimSpace(p.out.String())
	if len(s) <= limit {
		return s
	}
	return "…(earlier output omitted)…\n" + s[len(s)-limit:]
}

// pagesDir is the root of the route tree.
func (p *project) pagesDir() string {
	return filepath.Join(p.cfg.WebDir, "pages")
}

// rel renders a path relative to the project root for reporting.
func (p *project) rel(path string) string {
	if r, err := filepath.Rel(p.cfg.Dir, path); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(path)
}

// reservedRouteSegments cannot appear in a route path: each is claimed by the
// generator for something else.
var reservedRouteSegments = map[string]string{
	"__ws":         "reserved for the generated WebSocket endpoint of a reactive route",
	"static_embed": "reserved as the staging directory for embedded static files",
}

// cleanRoutePath validates a caller-supplied route path and returns it in
// canonical form ("/users/_userId_") together with its slash-separated relative
// form for joining onto the pages directory.
func cleanRoutePath(routePath string) (canonical string, relative string, err error) {
	p := strings.TrimSpace(routePath)
	if p == "" {
		return "", "", fmt.Errorf("routePath is required, for example \"/users/_userId_/orders\"")
	}

	p = filepath.ToSlash(p)
	if filepath.IsAbs(filepath.FromSlash(p)) && !strings.HasPrefix(p, "/") {
		return "", "", fmt.Errorf("routePath %q must be a route path such as \"/users\", not a filesystem path", routePath)
	}
	if strings.Contains(p, "\\") {
		return "", "", fmt.Errorf("routePath %q must use forward slashes", routePath)
	}

	segments := strings.Split(strings.Trim(p, "/"), "/")
	var kept []string
	for _, s := range segments {
		switch s {
		case "", ".":
			continue
		case "..":
			return "", "", fmt.Errorf("routePath %q must not contain \"..\"", routePath)
		}
		if reason, reserved := reservedRouteSegments[s]; reserved {
			return "", "", fmt.Errorf("route segment %q is %s", s, reason)
		}
		if err := checkRouteSegment(s); err != nil {
			return "", "", err
		}
		kept = append(kept, s)
	}

	if len(kept) == 0 {
		return "", "", fmt.Errorf("routePath %q resolves to the root route, which already exists", routePath)
	}

	return "/" + strings.Join(kept, "/"), strings.Join(kept, "/"), nil
}

// checkRouteSegment rejects a segment that cannot become a route directory. The
// directory name is also the name of the generated Go package and its import
// alias, so anything that is not a Go identifier — a hyphen, a leading digit, a
// keyword — turns into an unreadable code-formatting failure much later, with
// the offending directory already written.
func checkRouteSegment(s string) error {
	name := s
	if strings.HasPrefix(name, "_") && strings.HasSuffix(name, "_") && len(name) > 2 {
		if strings.Contains(name[1:len(name)-1], "_") {
			return fmt.Errorf("route segment %q looks like a URL parameter but contains an underscore inside the name; "+
				"the router only captures segments of the form _name_, so this one would be matched literally", s)
		}
		name = name[1 : len(name)-1]
	}

	if !token.IsIdentifier(name) {
		return fmt.Errorf("route segment %q cannot be a directory name in a GoSSR project: it becomes a Go package name, "+
			"so it must start with a letter or underscore and contain only letters, digits and underscores (\"userProfile\", not \"user-profile\")", s)
	}
	return nil
}

// routeDir resolves a validated route path to a directory inside pages/, and
// double-checks the result has not escaped it.
func (p *project) routeDir(relative string) (string, error) {
	pages, err := filepath.Abs(p.pagesDir())
	if err != nil {
		return "", err
	}

	dir := filepath.Join(pages, filepath.FromSlash(relative))
	if dir != pages && !strings.HasPrefix(dir, pages+string(filepath.Separator)) {
		return "", fmt.Errorf("route path %q resolves outside %s", relative, p.rel(pages))
	}
	return dir, nil
}
