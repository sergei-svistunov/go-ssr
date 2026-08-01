package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sergei-svistunov/go-ssr/internal/boilerplate"
	"github.com/sergei-svistunov/go-ssr/internal/generator"
)

func ptr[T any](v T) *T { return &v }

// assetStepName labels the one step of project creation that needs Node.
const assetStepName = "npm install and webpack"

// registerTools adds every tool to the server.
func registerTools(s *mcp.Server) {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(false)}
	writes := &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: ptr(false), OpenWorldHint: ptr(false)}

	mcp.AddTool(s, &mcp.Tool{
		Name:  "gossr_docs",
		Title: "Read GoSSR documentation",
		Description: "Return a GoSSR documentation topic. Read this before writing templates, data providers or " +
			"reactive code — the framework has its own template language and conventions.\n\nTopics:\n" + topicList(),
		InputSchema: docsInputSchema(),
		Annotations: readOnly,
	}, handleDocs)

	mcp.AddTool(s, &mcp.Tool{
		Name:  "gossr_search_docs",
		Title: "Search GoSSR documentation",
		Description: "Search the GoSSR documentation for a term such as an ssr: tag, an attribute, a generated " +
			"method name or an error message. Returns matching sections with their topic slugs for gossr_docs.",
		Annotations: readOnly,
	}, handleSearchDocs)

	mcp.AddTool(s, &mcp.Tool{
		Name:  "gossr_init",
		Title: "Create a GoSSR project",
		Description: "Scaffold a new GoSSR application in an empty directory, then install npm dependencies, " +
			"build assets, generate code and resolve Go modules, so the result runs immediately. " +
			"Set depsPackage/depsType now if the app needs dependencies injected into its data providers — " +
			"changing them later means editing every dataprovider.go by hand.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(true)},
	}, handleInit)

	mcp.AddTool(s, &mcp.Tool{
		Name:  "gossr_scaffold_route",
		Title: "Add a GoSSR route",
		Description: "Create a route directory with a starter index.html (plus optional index.ts and styles.scss), " +
			"then regenerate so the route's dataprovider.go stub exists. Reports the data provider methods the " +
			"route requires. Edit the template afterwards as needed and call gossr_generate again.",
		Annotations: writes,
	}, handleScaffoldRoute)

	mcp.AddTool(s, &mcp.Tool{
		Name:  "gossr_generate",
		Title: "Generate GoSSR code",
		Description: "Regenerate the Go and TypeScript code for a project after editing templates. Builds assets " +
			"first when their sources are newer than the last build. Returns located diagnostics when a template " +
			"or binding is invalid.",
		Annotations: writes,
	}, handleGenerate)

	mcp.AddTool(s, &mcp.Tool{
		Name:  "gossr_validate",
		Title: "Validate GoSSR templates",
		Description: "Check every template and reactive binding without writing any files. Returns one diagnostic " +
			"per broken route, each with file, line and error code.",
		Annotations: readOnly,
	}, handleValidate)

	mcp.AddTool(s, &mcp.Tool{
		Name:  "gossr_routes",
		Title: "Inspect GoSSR routes",
		Description: "Describe the project's routes: URL parameters, declared variables with their reactive flags, " +
			"forms and their fields, WebSocket endpoints, and the data provider methods each route requires. " +
			"Use it to understand an existing app before changing it.",
		Annotations: readOnly,
	}, handleRoutes)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "gossr_webpack",
		Title:       "Build GoSSR assets",
		Description: "Install npm dependencies when needed and run webpack for the project's TypeScript, styles and images.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: ptr(false), OpenWorldHint: ptr(true)},
	}, handleWebpack)

	mcp.AddTool(s, &mcp.Tool{
		Name:  "gossr_run",
		Title: "Run the GoSSR application",
		Description: "Start the application with `go run` in the background and wait until it answers or fails. " +
			"Generation only writes and formats Go code, so this is how to find out whether it compiles and serves. " +
			"The application keeps running after the call returns; use gossr_logs to follow it and gossr_stop to end it.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(true)},
	}, handleRun)

	mcp.AddTool(s, &mcp.Tool{
		Name:  "gossr_logs",
		Title: "Read the application log",
		Description: "Return what the running application has written since the last call, or everything buffered. " +
			"Use it after exercising a page to see the server-side error behind a failed request.",
		Annotations: readOnly,
	}, handleLogs)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "gossr_stop",
		Title:       "Stop the GoSSR application",
		Description: "Stop the application started by gossr_run, together with the binary `go run` built for it.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: ptr(false), OpenWorldHint: ptr(false)},
	}, handleStop)
}

// docsInputSchema pins the topic property to the slugs that actually exist, so a
// caller sees the whole corpus in the tool schema and cannot guess a bad slug.
func docsInputSchema() *jsonschema.Schema {
	slugs := topicSlugs()
	enum := make([]any, len(slugs))
	for i, s := range slugs {
		enum[i] = s
	}

	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"topic": {
				Type:        "string",
				Description: "documentation topic slug",
				Enum:        enum,
			},
			"section": {
				Type:        "string",
				Description: "optional heading within the topic; returns just that section",
			},
		},
		Required: []string{"topic"},
	}
}

// ---------------------------------------------------------------------------
// Documentation
// ---------------------------------------------------------------------------

func handleDocs(_ context.Context, _ *mcp.CallToolRequest, in docsInput) (*mcp.CallToolResult, docsOutput, error) {
	topic, err := findTopic(in.Topic)
	if err != nil {
		return nil, docsOutput{}, err
	}

	content, err := section(topic, in.Section)
	if err != nil {
		return nil, docsOutput{}, err
	}

	out := docsOutput{Topic: topic.Slug, Title: topic.Title, Section: in.Section, Content: content}
	return text(content), out, nil
}

func handleSearchDocs(_ context.Context, _ *mcp.CallToolRequest, in searchDocsInput) (*mcp.CallToolResult, searchDocsOutput, error) {
	hits := searchDocs(in.Query, in.Limit)
	out := searchDocsOutput{Query: in.Query, Hits: hits}

	var b strings.Builder
	if len(hits) == 0 {
		fmt.Fprintf(&b, "No documentation matches %q. Topics: %s", in.Query, strings.Join(topicSlugs(), ", "))
	} else {
		fmt.Fprintf(&b, "%d match(es) for %q:\n\n", len(hits), in.Query)
		for _, h := range hits {
			fmt.Fprintf(&b, "- `%s`", h.Topic)
			if h.Section != "" {
				fmt.Fprintf(&b, " → %s", h.Section)
			}
			fmt.Fprintf(&b, "\n  %s\n", h.Snippet)
		}
		fmt.Fprintf(&b, "\nFetch a topic with gossr_docs.")
	}

	return text(b.String()), out, nil
}

// ---------------------------------------------------------------------------
// Project creation
// ---------------------------------------------------------------------------

func handleInit(_ context.Context, _ *mcp.CallToolRequest, in initInput) (*mcp.CallToolResult, initOutput, error) {
	if strings.TrimSpace(in.Dir) == "" {
		return nil, initOutput{}, errors.New("dir is required")
	}
	if strings.TrimSpace(in.ModuleName) == "" {
		return nil, initOutput{}, errors.New("moduleName is required, for example example.com/myapp")
	}

	dir, err := filepath.Abs(in.Dir)
	if err != nil {
		return nil, initOutput{}, err
	}

	webDir := in.WebDir
	if webDir == "" {
		webDir = boilerplate.DefaultWebDir
	}

	opts := boilerplate.Options{
		Dir:         dir,
		PkgName:     in.ModuleName,
		WebDir:      webDir,
		DepsPackage: in.DepsPackage,
		DepsType:    in.DepsType,
	}

	// Files validates the options, so rejecting a bad request happens before the
	// target directory is created and does not leave one behind.
	created, err := boilerplate.Files(opts)
	if err != nil {
		return nil, initOutput{}, err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, initOutput{}, err
	}
	if err := boilerplate.Init(opts); err != nil {
		return nil, initOutput{}, fmt.Errorf("scaffolding %s: %w", dir, err)
	}

	out := initOutput{ProjectDir: dir, Module: in.ModuleName, WebDir: webDir, Created: created}

	p, err := openProject(dir, false)
	if err != nil {
		return nil, out, err
	}

	// Each step is reported even when a later one fails, so a partial result is
	// diagnosable instead of looking like a total failure. The asset build needs
	// Node, which the code generation does not: a machine without it should
	// still end up with a project whose Go code compiles.
	steps := []struct {
		name     string
		run      func() error
		required bool
	}{
		{assetStepName, p.gen.Webpack, false},
		{"analyze templates", p.gen.Analyze, true},
		{"generate code", p.gen.Generate, true},
		{"go mod tidy", p.gen.GoModTidy, true},
	}

	out.OK = true
	for _, s := range steps {
		err := s.run()
		st := step{Name: s.name, OK: err == nil}
		if err != nil {
			st.Detail = err.Error()
			out.OK = false
		}
		out.Steps = append(out.Steps, st)
		if err != nil && s.required {
			break
		}
	}

	out.Output = p.output()
	if out.OK {
		out.NextSteps = []string{
			fmt.Sprintf("cd %s && go run .   # serves http://localhost:8080/", in.Dir),
			"gossr_docs topic=recipes/new-app for the layout, pages and dependency-injection walkthrough",
		}
	} else if assetBuildIsTheOnlyFailure(out.Steps) {
		out.NextSteps = []string{
			"install Node and npm, then call gossr_webpack — the Go code is generated, but pages render without their CSS and JS until the assets are built",
		}
	}

	var b strings.Builder
	if out.OK {
		fmt.Fprintf(&b, "Created GoSSR project %s in %s (%d files).\n", in.ModuleName, dir, len(created))
	} else {
		fmt.Fprintf(&b, "Scaffolded %s in %s, but a follow-up step failed.\n", in.ModuleName, dir)
	}
	for _, s := range out.Steps {
		mark := "ok"
		if !s.OK {
			mark = "FAILED"
		}
		fmt.Fprintf(&b, "  %-24s %s\n", s.Name, mark)
		if s.Detail != "" {
			fmt.Fprintf(&b, "    %s\n", s.Detail)
		}
	}
	for _, n := range out.NextSteps {
		fmt.Fprintf(&b, "\nNext: %s", n)
	}
	if !out.OK && out.Output != "" {
		fmt.Fprintf(&b, "\n\nTool output:\n%s", out.Output)
	}

	return result(out.OK, b.String()), out, nil
}

// assetBuildIsTheOnlyFailure reports whether a project was scaffolded and its
// code generated, and only the Node-dependent asset build failed.
func assetBuildIsTheOnlyFailure(steps []step) bool {
	failed := 0
	for _, s := range steps {
		if !s.OK {
			failed++
			if s.Name != assetStepName {
				return false
			}
		}
	}
	return failed == 1
}

// ---------------------------------------------------------------------------
// Route scaffolding
// ---------------------------------------------------------------------------

func handleScaffoldRoute(_ context.Context, _ *mcp.CallToolRequest, in scaffoldRouteInput) (*mcp.CallToolResult, scaffoldRouteOutput, error) {
	canonical, relative, err := cleanRoutePath(in.RoutePath)
	if err != nil {
		return nil, scaffoldRouteOutput{}, err
	}

	policy, err := parseAssetPolicy(in.Assets)
	if err != nil {
		return nil, scaffoldRouteOutput{}, err
	}

	p, err := openProject(in.ProjectDir, false)
	if err != nil {
		return nil, scaffoldRouteOutput{}, err
	}

	dir, err := p.routeDir(relative)
	if err != nil {
		return nil, scaffoldRouteOutput{}, err
	}

	indexPath := filepath.Join(dir, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		return nil, scaffoldRouteOutput{}, fmt.Errorf("route %s already exists at %s; edit the template instead", canonical, p.rel(indexPath))
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, scaffoldRouteOutput{}, err
	}

	out := scaffoldRouteOutput{RoutePath: canonical, URLParams: generator.URLParams(canonical)}

	files := map[string]string{
		indexPath: starterTemplate(canonical, in.Heading, in.HasChildren),
	}
	if in.WithTs {
		files[filepath.Join(dir, "index.ts")] = starterTypeScript(canonical)
	}
	if in.WithScss {
		files[filepath.Join(dir, "styles.scss")] = starterStyles(canonical)
	}

	for path, content := range files {
		if _, err := os.Stat(path); err == nil {
			continue // never clobber a file the caller already has
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return nil, out, err
		}
		out.Created = append(out.Created, p.rel(path))
	}
	sortStrings(out.Created)

	// Regenerating is what produces the dataprovider.go stub for the new route.
	diags, genErr := p.generate(policy)
	if genErr != nil {
		// The template is already on disk. Returning a bare error would drop the
		// structured result along with it, and the caller's natural retry would
		// then be refused because the route now exists.
		out.Output = p.output()

		var b strings.Builder
		fmt.Fprintf(&b, "Route %s: the files were created but regeneration failed: %v\n", canonical, genErr)
		for _, f := range out.Created {
			fmt.Fprintf(&b, "  created %s\n", f)
		}
		b.WriteString("\nFix the cause and call gossr_generate — calling gossr_scaffold_route again will refuse, because the route exists now.\n")
		if out.Output != "" {
			fmt.Fprintf(&b, "\nTool output:\n%s", out.Output)
		}

		return result(false, b.String()), out, nil
	}
	out.Diagnostics = diags
	out.OK = len(diags) == 0 && !p.assetsFailed

	dpPath := filepath.Join(dir, "dataprovider.go")
	if _, err := os.Stat(dpPath); err == nil {
		out.DataProvider = p.rel(dpPath)
	}

	if inv, err := p.gen.Inventory(canonical); err == nil && len(inv.Routes) == 1 {
		out.DataProviderMethods = inv.Routes[0].DataProviderMethods
	}

	if out.OK {
		out.NextSteps = []string{
			fmt.Sprintf("implement %s in %s", strings.Join(out.DataProviderMethods, ", "), out.DataProvider),
			"declare the values the template needs with <ssr:var name=\"…\" type=\"…\"/>, then call gossr_generate",
		}
		if p.routeHasParent(canonical) {
			out.NextSteps = append(out.NextSteps, "make sure the parent template has an <ssr:content/> slot")
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Route %s\n", canonical)
	for _, f := range out.Created {
		fmt.Fprintf(&b, "  created %s\n", f)
	}
	if out.DataProvider != "" {
		fmt.Fprintf(&b, "  data provider %s\n", out.DataProvider)
	}
	if len(out.URLParams) > 0 {
		fmt.Fprintf(&b, "  URL parameters: %s (read with r.URLParam)\n", strings.Join(out.URLParams, ", "))
	}
	if len(out.DataProviderMethods) > 0 {
		fmt.Fprintf(&b, "  methods to implement: %s\n", strings.Join(out.DataProviderMethods, ", "))
	}
	writeAssetNote(&b, p)
	writeDiagnostics(&b, diags)
	for _, n := range out.NextSteps {
		fmt.Fprintf(&b, "\nNext: %s", n)
	}

	out.Output = p.output()
	return result(out.OK, b.String()), out, nil
}

// routeHasParent reports whether the route renders inside another route, which
// is when an <ssr:content/> slot in the parent matters.
func (p *project) routeHasParent(canonical string) bool {
	return strings.Count(canonical, "/") >= 1 && canonical != "/"
}

// ---------------------------------------------------------------------------
// Generate, validate, inspect, build
// ---------------------------------------------------------------------------

func handleGenerate(_ context.Context, _ *mcp.CallToolRequest, in generateInput) (*mcp.CallToolResult, generateOutput, error) {
	p, err := openProject(in.ProjectDir, in.Prod)
	if err != nil {
		return nil, generateOutput{}, err
	}

	policy, err := parseAssetPolicy(in.Assets)
	if err != nil {
		return nil, generateOutput{}, err
	}

	out := generateOutput{ProjectDir: p.cfg.Dir}

	stubsBefore := p.dataProviderPaths()

	diags, err := p.generate(policy)
	if err != nil {
		return nil, out, err
	}

	out.Diagnostics = diags
	// A failed asset build is not a diagnostic, but the pages it leaves behind
	// have no CSS or JavaScript. The command-line generator stops on it, so a
	// caller here must not read the run as clean either.
	out.OK = len(diags) == 0 && !p.assetsFailed
	out.WebpackRan = p.webpackRan
	out.AssetsReason = p.assetsReason
	out.Routes = p.gen.RoutePaths()
	out.Output = p.output()

	for path := range p.dataProviderPaths() {
		if !stubsBefore[path] {
			out.StubsCreated = append(out.StubsCreated, p.rel(path))
		}
	}
	sortStrings(out.StubsCreated)

	var b strings.Builder
	switch {
	case out.OK:
		fmt.Fprintf(&b, "Generated %d route(s) in %s.\n", len(out.Routes), p.rel(p.pagesDir()))
	case len(diags) > 0:
		fmt.Fprintf(&b, "Generation reported %d problem(s).\n", len(diags))
	default:
		fmt.Fprintf(&b, "Generated %d route(s) in %s, but the asset build failed.\n", len(out.Routes), p.rel(p.pagesDir()))
	}
	writeAssetNote(&b, p)
	for _, s := range out.StubsCreated {
		fmt.Fprintf(&b, "  new data provider stub %s\n", s)
	}
	writeDiagnostics(&b, diags)
	if !out.OK && out.Output != "" {
		fmt.Fprintf(&b, "\nTool output:\n%s", out.Output)
	}

	return result(out.OK, b.String()), out, nil
}

func handleValidate(_ context.Context, _ *mcp.CallToolRequest, in projectInput) (*mcp.CallToolResult, validateOutput, error) {
	p, err := openProject(in.ProjectDir, false)
	if err != nil {
		return nil, validateOutput{}, err
	}

	diags, err := p.gen.Validate()
	if err != nil {
		return nil, validateOutput{}, err
	}

	out := validateOutput{
		OK:          len(diags) == 0,
		ProjectDir:  p.cfg.Dir,
		Routes:      p.gen.RoutePaths(),
		Diagnostics: diags,
	}

	stale, reason, err := p.gen.AssetsStale()
	if err == nil {
		out.AssetsStale = stale
		out.AssetsNote = reason
	}

	var b strings.Builder
	if out.OK {
		fmt.Fprintf(&b, "%d route(s), no problems found.\n", len(out.Routes))
	} else {
		fmt.Fprintf(&b, "%d problem(s) in %d route(s).\n", len(diags), len(out.Routes))
	}
	writeDiagnostics(&b, diags)
	if out.AssetsStale {
		fmt.Fprintf(&b, "\nAssets: %s — call gossr_webpack or gossr_generate.", out.AssetsNote)
	}

	return result(out.OK, b.String()), out, nil
}

func handleRoutes(_ context.Context, _ *mcp.CallToolRequest, in routesInput) (*mcp.CallToolResult, routesOutput, error) {
	p, err := openProject(in.ProjectDir, false)
	if err != nil {
		return nil, routesOutput{}, err
	}

	// Analysis failures do not hide the routes that did parse.
	diags, hardErr := splitDiagnostics(p.gen.Analyze())
	if hardErr != nil {
		return nil, routesOutput{}, hardErr
	}

	inv, err := p.gen.Inventory(in.RoutePath)
	if err != nil {
		// A route whose template failed to parse is missing from the analyzed
		// set. Reporting it as unknown would send the caller off to re-create a
		// file that is already there; report what is wrong with it instead.
		if own := diagnosticsFor(diags, in.RoutePath); len(own) > 0 {
			out := routesOutput{Inventory: generator.Inventory{}, Diagnostics: own}

			var b strings.Builder
			fmt.Fprintf(&b, "Route %s could not be described because its template did not parse.\n", in.RoutePath)
			writeDiagnostics(&b, own)

			return result(false, b.String()), out, nil
		}
		return nil, routesOutput{}, err
	}

	out := routesOutput{OK: len(diags) == 0, Inventory: inv, Diagnostics: diags}

	var b strings.Builder
	if len(inv.Routes) == 0 {
		b.WriteString("No routes found. A route is a directory under pages/ containing an index.html.\n")
	}
	if len(inv.WSEndpoints) > 0 {
		fmt.Fprintf(&b, "WebSocket endpoints (one connection per page, shared by the whole route stack):\n")
		for _, leaf := range inv.WSEndpoints {
			fmt.Fprintf(&b, "  %s/__ws\n", strings.TrimSuffix(leaf, "/"))
		}
		b.WriteString("\n")
	}
	for _, r := range inv.Routes {
		fmt.Fprintf(&b, "%s\n", r.Path)
		fmt.Fprintf(&b, "  template %s (package %s)\n", r.Template, r.Package)
		if len(r.URLParams) > 0 {
			fmt.Fprintf(&b, "  url params: %s\n", strings.Join(r.URLParams, ", "))
		}
		if r.HasContentSlot {
			slot := "child routes render here"
			if r.ContentDefault != "" {
				slot += ", default " + r.ContentDefault
			}
			fmt.Fprintf(&b, "  ssr:content: %s\n", slot)
		}
		if r.Reactive {
			if r.WSEndpoint != "" {
				fmt.Fprintf(&b, "  reactive, websocket %s\n", r.WSEndpoint)
			} else {
				b.WriteString("  reactive; patches travel over the endpoint of whichever leaf route is being viewed\n")
			}
		}
		for _, v := range r.Variables {
			flags := ""
			if v.Reactive {
				flags = " reactive"
			}
			if v.ClientWritable {
				flags += " client-writable"
			}
			fmt.Fprintf(&b, "  var %s %s%s\n", v.Name, v.Type, flags)
		}
		for _, f := range r.Forms {
			fields := make([]string, len(f.Fields))
			for i, fl := range f.Fields {
				fields[i] = fmt.Sprintf("%s %s/%s", fl.Name, fl.GoType, fl.Kind)
			}
			fmt.Fprintf(&b, "  form %s {%s} → %s, %s\n", f.Name, strings.Join(fields, ", "), f.InitMethod, f.ProcessMethod)
		}
		fmt.Fprintf(&b, "  data provider: %s", strings.Join(r.DataProviderMethods, ", "))
		if !r.HasDataProvider {
			b.WriteString(" (dataprovider.go missing — run gossr_generate)")
		}
		b.WriteString("\n")
	}
	writeDiagnostics(&b, diags)

	return result(out.OK, b.String()), out, nil
}

// diagnosticsFor returns the diagnostics that belong to one route.
func diagnosticsFor(diags []generator.Diagnostic, routePath string) []generator.Diagnostic {
	if routePath == "" {
		return nil
	}
	want := path.Join("/", strings.Trim(routePath, "/"))

	var own []generator.Diagnostic
	for _, d := range diags {
		if d.Route == want {
			own = append(own, d)
		}
	}
	return own
}

func handleWebpack(_ context.Context, _ *mcp.CallToolRequest, in webpackInput) (*mcp.CallToolResult, webpackOutput, error) {
	p, err := openProject(in.ProjectDir, in.Prod)
	if err != nil {
		return nil, webpackOutput{}, err
	}

	out := webpackOutput{ProjectDir: p.cfg.Dir, Prod: in.Prod}
	err = p.gen.Webpack()
	out.OK = err == nil
	out.Output = p.output()

	var b strings.Builder
	if out.OK {
		mode := "development"
		if in.Prod {
			mode = "production"
		}
		fmt.Fprintf(&b, "Assets built in %s mode.\n", mode)
	} else {
		fmt.Fprintf(&b, "Asset build failed: %v\n", err)
	}
	if out.Output != "" {
		fmt.Fprintf(&b, "\n%s", out.Output)
	}

	return result(out.OK, b.String()), out, nil
}

// ---------------------------------------------------------------------------
// Running the application
// ---------------------------------------------------------------------------

func handleRun(_ context.Context, _ *mcp.CallToolRequest, in runInput) (*mcp.CallToolResult, runOutput, error) {
	p, err := openProject(in.ProjectDir, false)
	if err != nil {
		return nil, runOutput{}, err
	}

	wait := defaultWait
	if in.WaitSeconds > 0 {
		wait = min(time.Duration(in.WaitSeconds)*time.Second, maxWait)
	}

	a, err := startApp(p)
	if err != nil {
		return nil, runOutput{ProjectDir: p.cfg.Dir}, err
	}
	waitReady(a, wait)

	running, code, _ := a.state()
	out := runOutput{
		ProjectDir: p.cfg.Dir,
		Running:    running,
		URL:        a.serviceURL(),
		Log:        a.log.excerpt(),
	}
	if running {
		out.PID = a.cmd.Process.Pid
	} else {
		out.ExitCode = code
	}
	out.OK = running

	// Whatever a caller reads here should not come back again from gossr_logs.
	_, _ = a.log.since()

	var b strings.Builder
	switch {
	case !running:
		fmt.Fprintf(&b, "The application did not stay up: %s.\n", describeExit(a))
		b.WriteString("The output below is the whole reason — a compile error, a port already in use, or a panic on startup.\n")
	case out.URL != "":
		fmt.Fprintf(&b, "The application is running (pid %d) and answering at %s.\n", out.PID, out.URL)
	default:
		fmt.Fprintf(&b, "The application is running (pid %d), but it printed no address to check within %s.\n",
			out.PID, wait)
		b.WriteString("It may still be starting, or it may not log the address it listens on. Call gossr_logs to follow it.\n")
	}
	if out.Log != "" {
		fmt.Fprintf(&b, "\nOutput:\n%s", out.Log)
	}

	return result(out.OK, b.String()), out, nil
}

func handleLogs(_ context.Context, _ *mcp.CallToolRequest, in logsInput) (*mcp.CallToolResult, logsOutput, error) {
	p, err := openProject(in.ProjectDir, false)
	if err != nil {
		return nil, logsOutput{}, err
	}

	a := lookupApp(p.cfg.Dir)
	if a == nil {
		return nil, logsOutput{ProjectDir: p.cfg.Dir},
			fmt.Errorf("no application has been started for %s; call gossr_run first", p.cfg.Dir)
	}

	out := logsOutput{ProjectDir: p.cfg.Dir, URL: a.serviceURL()}
	if in.All {
		out.Log = a.log.all()
	} else {
		out.Log, out.MissedBytes = a.log.since()
	}

	running, code, _ := a.state()
	out.Running = running
	out.OK = running
	if running {
		out.PID = a.cmd.Process.Pid
		out.UptimeSecond = int(time.Since(a.started).Seconds())
	} else {
		out.ExitCode = code
	}

	var b strings.Builder
	if running {
		fmt.Fprintf(&b, "Running (pid %d, up %ds)", out.PID, out.UptimeSecond)
		if out.URL != "" {
			fmt.Fprintf(&b, " at %s", out.URL)
		}
		b.WriteString(".\n")
	} else {
		fmt.Fprintf(&b, "Not running: %s.\n", describeExit(a))
	}
	if out.MissedBytes > 0 {
		fmt.Fprintf(&b, "%d earlier bytes scrolled out of the buffer before they were read.\n", out.MissedBytes)
	}
	if strings.TrimSpace(out.Log) == "" {
		b.WriteString("\nNo new output.\n")
	} else {
		fmt.Fprintf(&b, "\n%s", out.Log)
	}

	return result(out.OK, b.String()), out, nil
}

func handleStop(_ context.Context, _ *mcp.CallToolRequest, in projectInput) (*mcp.CallToolResult, stopOutput, error) {
	p, err := openProject(in.ProjectDir, false)
	if err != nil {
		return nil, stopOutput{}, err
	}

	a, stopped := stopApp(p.cfg.Dir)

	// Stopping something that is not running is the state the caller asked for,
	// not a failure.
	out := stopOutput{OK: true, ProjectDir: p.cfg.Dir, Stopped: stopped}
	if a != nil {
		_, out.ExitCode, _ = a.state()
		out.Log = a.log.excerpt()
	}

	var b strings.Builder
	switch {
	case stopped:
		b.WriteString("The application was stopped.\n")
	case a != nil:
		fmt.Fprintf(&b, "Nothing to stop: %s.\n", describeExit(a))
	default:
		b.WriteString("Nothing to stop: no application has been started for this project.\n")
	}
	if out.Log != "" {
		fmt.Fprintf(&b, "\nLast output:\n%s", out.Log)
	}

	return result(out.OK, b.String()), out, nil
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

func text(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// result marks a content failure as a tool error so a caller cannot mistake it
// for success, while still returning the structured payload.
func result(ok bool, s string) *mcp.CallToolResult {
	r := text(s)
	r.IsError = !ok
	return r
}

// writeAssetNote explains what happened to the asset build, which is the one
// part of a generation run that can quietly leave a page without its CSS or JS.
func writeAssetNote(b *strings.Builder, p *project) {
	switch {
	case p.webpackRan:
		fmt.Fprintf(b, "Assets rebuilt (%s).\n", p.assetsReason)
	case p.assetsFailed:
		fmt.Fprintf(b, "WARNING: assets are stale and could not be built: %s\n"+
			"Code was generated anyway; pages will render without their CSS and JS until the build succeeds. "+
			"Check that Node and npm are installed, then call gossr_webpack.\n", p.assetsReason)
	case p.assetsReason != "":
		fmt.Fprintf(b, "Assets not built: %s\n", p.assetsReason)
	}
}

func writeDiagnostics(b *strings.Builder, diags []generator.Diagnostic) {
	if len(diags) == 0 {
		return
	}
	b.WriteString("\nDiagnostics:\n")
	for _, d := range diags {
		fmt.Fprintf(b, "  %s\n", (&d).Error())
	}
	b.WriteString("\nSee gossr_docs topic=errors for what these mean.\n")
}

// splitDiagnostics separates problems with the project's content from
// operational failures such as an unreadable directory.
func splitDiagnostics(err error) ([]generator.Diagnostic, error) {
	if err == nil {
		return nil, nil
	}
	var ds generator.Diagnostics
	if errors.As(err, &ds) {
		return ds, nil
	}
	var d *generator.Diagnostic
	if errors.As(err, &d) {
		return []generator.Diagnostic{*d}, nil
	}
	return nil, err
}
