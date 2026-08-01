package generator

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	routepkg "github.com/sergei-svistunov/go-ssr/internal/generator/route"
	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template"
)

// Inventory is a description of everything a project's templates declare. It
// answers "what does this app contain, and what do I still have to implement?"
// without the caller having to read and interpret every template itself.
type Inventory struct {
	ProjectDir  string      `json:"projectDir"`
	WebDir      string      `json:"webDir"`
	Routes      []RouteInfo `json:"routes"`
	WSEndpoints []string    `json:"wsEndpoints,omitempty"`
}

// RouteInfo describes a single route.
type RouteInfo struct {
	Path           string   `json:"path"`
	Template       string   `json:"template"`
	Package        string   `json:"package"`
	URLParams      []string `json:"urlParams,omitempty"`
	HasContentSlot bool     `json:"hasContentSlot"`
	ContentDefault string   `json:"contentDefault,omitempty"`
	Reactive       bool     `json:"reactive"`
	WSEndpoint     string   `json:"wsEndpoint,omitempty"`

	Variables []VariableInfo `json:"variables,omitempty"`
	Forms     []FormInfo     `json:"forms,omitempty"`

	// DataProviderMethods lists the methods the route's RouteDataProvider
	// interface requires. The generated dataprovider.go stub starts with all of
	// them; the compiler enforces the set from then on.
	DataProviderMethods []string `json:"dataProviderMethods"`
	HasDataProvider     bool     `json:"hasDataProvider"`
}

// VariableInfo describes an <ssr:var> declaration.
type VariableInfo struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	Reactive       bool   `json:"reactive"`
	ClientWritable bool   `json:"clientWritable"`
	Line           int    `json:"line,omitempty"`
}

// FormInfo describes an <ssr:form> and the two hooks it generates.
type FormInfo struct {
	Name          string          `json:"name"`
	EncType       string          `json:"encType,omitempty"`
	InitMethod    string          `json:"initMethod"`
	ProcessMethod string          `json:"processMethod"`
	ValuesType    string          `json:"valuesType"`
	Fields        []FormFieldInfo `json:"fields,omitempty"`
}

// FormFieldInfo describes one form element.
type FormFieldInfo struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	GoType   string `json:"goType"`
	Required bool   `json:"required"`
	Multiple bool   `json:"multiple"`
}

var formElementKinds = map[template.FormElementType]string{
	template.FormElementInput:     "input",
	template.FormElementInputFile: "file",
	template.FormElementTextarea:  "textarea",
	template.FormElementSelect:    "select",
}

// RoutePaths returns the paths of the analyzed routes, sorted.
func (g *Generator) RoutePaths() []string {
	return g.getRoutesPaths()
}

// Inventory returns the route inventory for the analyzed project. Pass an empty
// routePath for every route, or a single route path such as "/users/_userId_"
// to describe just that one.
//
// Analyze must have run first.
func (g *Generator) Inventory(routePath string) (Inventory, error) {
	inv := Inventory{
		ProjectDir:  g.dir,
		WebDir:      g.relPath(g.webDir),
		WSEndpoints: g.computeWSLeafPaths(),
	}

	if routePath != "" {
		routePath = path.Join("/", routePath)
		if _, ok := g.routes[routePath]; !ok {
			return Inventory{}, fmt.Errorf("route %q not found; known routes: %s", routePath, strings.Join(g.getRoutesPaths(), ", "))
		}
	}

	wsEndpointByRoute := make(map[string]string, len(inv.WSEndpoints))
	for _, leaf := range inv.WSEndpoints {
		wsEndpointByRoute[leaf] = path.Join(leaf, "__ws")
	}

	for _, rPath := range g.getRoutesPaths() {
		if routePath != "" && rPath != routePath {
			continue
		}
		inv.Routes = append(inv.Routes, g.routeInfo(rPath, g.routes[rPath], wsEndpointByRoute[rPath]))
	}

	return inv, nil
}

func (g *Generator) routeInfo(rPath string, r *routepkg.Route, wsEndpoint string) RouteInfo {
	info := RouteInfo{
		Path:            rPath,
		Template:        g.relPath(g.routeIndexPath(rPath)),
		Package:         path.Base(path.Join("pages", strings.TrimPrefix(rPath, "/"))),
		URLParams:       urlParams(rPath),
		Reactive:        isReactiveRoute(r),
		WSEndpoint:      wsEndpoint,
		HasDataProvider: fileExists(filepath.Join(filepath.Dir(g.routeIndexPath(rPath)), "dataprovider.go")),
	}

	tmpl := r.Template()
	if tmpl == nil {
		return info
	}

	if content := tmpl.GetContentNode(); content != nil {
		info.HasContentSlot = true
		info.ContentDefault = content.Default
	}

	for _, v := range tmpl.GetVariables() {
		info.Variables = append(info.Variables, VariableInfo{
			Name:           v.Name,
			Type:           v.Type,
			Reactive:       v.Reactive,
			ClientWritable: v.ClientWritable,
			Line:           v.Line,
		})
	}

	for _, f := range tmpl.GetForms() {
		form := FormInfo{
			Name:          f.Name,
			InitMethod:    getFormInitMethod(f.Name),
			ProcessMethod: getFormProcessMethod(f.Name),
			ValuesType:    getFormValuesTypeName(f.Name),
		}
		if f.Node != nil {
			form.EncType = f.Node.EncType
		}
		for _, e := range f.Elements {
			form.Fields = append(form.Fields, FormFieldInfo{
				Name:     e.Name,
				Kind:     formElementKinds[e.Type],
				GoType:   e.GoType,
				Required: e.IsRequired,
				Multiple: e.IsMultiple,
			})
		}
		info.Forms = append(info.Forms, form)
	}

	info.DataProviderMethods = dataProviderMethods(tmpl, info.Reactive)

	return info
}

// dataProviderMethods mirrors the RouteDataProvider interface the generator
// emits for a route, so a caller learns what to implement without compiling.
func dataProviderMethods(tmpl *template.Template, reactive bool) []string {
	methods := []string{"Data"}

	if content := tmpl.GetContentNode(); content != nil && content.Default == "" {
		methods = append(methods, "DefaultRoute")
	}

	for _, f := range tmpl.GetForms() {
		methods = append(methods, getFormInitMethod(f.Name), getFormProcessMethod(f.Name))
	}

	if reactive {
		methods = append(methods, "Subscribe")
		for _, v := range clientWritableVars(tmpl.GetVariables()) {
			methods = append(methods, "Validate"+getExportedName(v.Name))
		}
	}

	return methods
}

// URLParams lists the parameters a route path captures. It applies the same
// rule as the router, so a caller cannot report a segment as dynamic that will
// be matched literally at runtime.
func URLParams(routePath string) []string {
	return urlParams(routePath)
}

// urlParams extracts the dynamic segments of a route path: a folder named
// _userId_ becomes the parameter userId.
func urlParams(rPath string) []string {
	var params []string
	for _, seg := range splitRoutePath(rPath) {
		if isParamSegment(seg) {
			params = append(params, strings.Trim(seg, "_"))
		}
	}
	return params
}
