package mcp

import "github.com/sergei-svistunov/go-ssr/internal/generator"

// Tool inputs. The SDK infers each tool's JSON schema from these types, and the
// jsonschema tags become the property descriptions a caller sees.

type docsInput struct {
	Topic   string `json:"topic" jsonschema:"documentation topic slug, for example template-syntax or recipes/add-form"`
	Section string `json:"section,omitempty" jsonschema:"optional heading within the topic; returns just that section"`
}

type searchDocsInput struct {
	Query string `json:"query" jsonschema:"text to look for, for example ssr:bind or Validate"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum number of matches to return (default 10)"`
}

type initInput struct {
	Dir         string `json:"dir" jsonschema:"directory to create the project in; created if missing and must be empty"`
	ModuleName  string `json:"moduleName" jsonschema:"Go module path for the new project, for example example.com/myapp"`
	WebDir      string `json:"webDir,omitempty" jsonschema:"where templates and assets live, relative to the project (default internal/web)"`
	DepsPackage string `json:"depsPackage,omitempty" jsonschema:"import path of a package whose type is injected into every data provider; it must not import the pages tree"`
	DepsType    string `json:"depsType,omitempty" jsonschema:"type name inside depsPackage (default Deps)"`
}

type scaffoldRouteInput struct {
	ProjectDir  string `json:"projectDir,omitempty" jsonschema:"path inside the project; defaults to the working directory"`
	RoutePath   string `json:"routePath" jsonschema:"route to create, for example /users/_userId_/orders; a segment wrapped in underscores is a URL parameter"`
	HasChildren bool   `json:"hasChildren,omitempty" jsonschema:"true when this route will be a layout for child routes; adds an ssr:content slot"`
	WithTs      bool   `json:"withTs,omitempty" jsonschema:"also create index.ts for this route"`
	WithScss    bool   `json:"withScss,omitempty" jsonschema:"also create styles.scss for this route"`
	Heading     string `json:"heading,omitempty" jsonschema:"optional heading text for the starter template"`
	Assets      string `json:"assets,omitempty" jsonschema:"asset build policy for the regeneration that follows: auto (default), skip, or force"`
}

type generateInput struct {
	ProjectDir string `json:"projectDir,omitempty" jsonschema:"path inside the project; defaults to the working directory"`
	Assets     string `json:"assets,omitempty" jsonschema:"asset build policy: auto (default, build only when sources are newer), skip, or force"`
	Prod       bool   `json:"prod,omitempty" jsonschema:"build assets in production mode and embed them"`
}

type projectInput struct {
	ProjectDir string `json:"projectDir,omitempty" jsonschema:"path inside the project; defaults to the working directory"`
}

type routesInput struct {
	ProjectDir string `json:"projectDir,omitempty" jsonschema:"path inside the project; defaults to the working directory"`
	RoutePath  string `json:"routePath,omitempty" jsonschema:"describe only this route, for example /users"`
}

type webpackInput struct {
	ProjectDir string `json:"projectDir,omitempty" jsonschema:"path inside the project; defaults to the working directory"`
	Prod       bool   `json:"prod,omitempty" jsonschema:"build in production mode"`
}

type runInput struct {
	ProjectDir  string `json:"projectDir,omitempty" jsonschema:"path inside the project; defaults to the working directory"`
	WaitSeconds int    `json:"waitSeconds,omitempty" jsonschema:"how long to wait for the application to answer before returning (default 20, maximum 120); it keeps running either way"`
}

type logsInput struct {
	ProjectDir string `json:"projectDir,omitempty" jsonschema:"path inside the project; defaults to the working directory"`
	All        bool   `json:"all,omitempty" jsonschema:"return everything buffered instead of only what is new since the last call"`
}

// Tool outputs.

type docsOutput struct {
	Topic   string `json:"topic"`
	Title   string `json:"title"`
	Section string `json:"section,omitempty"`
	Content string `json:"content"`
}

type searchDocsOutput struct {
	Query string      `json:"query"`
	Hits  []SearchHit `json:"hits"`
}

type step struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type initOutput struct {
	OK         bool     `json:"ok"`
	ProjectDir string   `json:"projectDir"`
	Module     string   `json:"module"`
	WebDir     string   `json:"webDir"`
	Created    []string `json:"created"`
	Steps      []step   `json:"steps"`
	NextSteps  []string `json:"nextSteps,omitempty"`
	Output     string   `json:"output,omitempty"`
}

type scaffoldRouteOutput struct {
	OK                  bool                   `json:"ok"`
	RoutePath           string                 `json:"routePath"`
	Created             []string               `json:"created"`
	URLParams           []string               `json:"urlParams,omitempty"`
	DataProvider        string                 `json:"dataProvider,omitempty"`
	DataProviderMethods []string               `json:"dataProviderMethods,omitempty"`
	Diagnostics         []generator.Diagnostic `json:"diagnostics,omitempty"`
	NextSteps           []string               `json:"nextSteps,omitempty"`
	Output              string                 `json:"output,omitempty"`
}

type generateOutput struct {
	OK           bool                   `json:"ok"`
	ProjectDir   string                 `json:"projectDir"`
	Routes       []string               `json:"routes,omitempty"`
	StubsCreated []string               `json:"stubsCreated,omitempty"`
	WebpackRan   bool                   `json:"webpackRan"`
	AssetsReason string                 `json:"assetsReason,omitempty"`
	Diagnostics  []generator.Diagnostic `json:"diagnostics,omitempty"`
	Output       string                 `json:"output,omitempty"`
}

type validateOutput struct {
	OK          bool                   `json:"ok"`
	ProjectDir  string                 `json:"projectDir"`
	Routes      []string               `json:"routes,omitempty"`
	AssetsStale bool                   `json:"assetsStale"`
	AssetsNote  string                 `json:"assetsNote,omitempty"`
	Diagnostics []generator.Diagnostic `json:"diagnostics,omitempty"`
}

type routesOutput struct {
	OK          bool                   `json:"ok"`
	Inventory   generator.Inventory    `json:"inventory"`
	Diagnostics []generator.Diagnostic `json:"diagnostics,omitempty"`
}

type webpackOutput struct {
	OK         bool   `json:"ok"`
	ProjectDir string `json:"projectDir"`
	Prod       bool   `json:"prod"`
	Output     string `json:"output,omitempty"`
}

type runOutput struct {
	OK         bool   `json:"ok"`
	ProjectDir string `json:"projectDir"`
	Running    bool   `json:"running"`
	PID        int    `json:"pid,omitempty"`
	URL        string `json:"url,omitempty"`
	ExitCode   int    `json:"exitCode,omitempty"`
	Log        string `json:"log,omitempty"`
}

type logsOutput struct {
	OK           bool   `json:"ok"`
	ProjectDir   string `json:"projectDir"`
	Running      bool   `json:"running"`
	PID          int    `json:"pid,omitempty"`
	URL          string `json:"url,omitempty"`
	ExitCode     int    `json:"exitCode,omitempty"`
	Log          string `json:"log"`
	MissedBytes  int64  `json:"missedBytes,omitempty"`
	UptimeSecond int    `json:"uptimeSeconds,omitempty"`
}

type stopOutput struct {
	OK         bool   `json:"ok"`
	ProjectDir string `json:"projectDir"`
	Stopped    bool   `json:"stopped"`
	ExitCode   int    `json:"exitCode,omitempty"`
	Log        string `json:"log,omitempty"`
}
