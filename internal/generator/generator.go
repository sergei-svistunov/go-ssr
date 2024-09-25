package generator

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
	"github.com/sergei-svistunov/go-ssr/internal/generator/route"
)

type Generator struct {
	dir     string
	pkgName string
	routes  map[string]*route.Route
	assets  *Assets
}

func New(dir, pkgName string) *Generator {
	return &Generator{
		dir:     dir,
		pkgName: pkgName,
		routes:  make(map[string]*route.Route),
	}
}

func (g *Generator) Analyze() error {
	g.routes = make(map[string]*route.Route)

	assets, err := AssetsFromDir(g.dir)
	if err != nil {
		return err
	}
	g.assets = assets

	pagesDir := filepath.Join(g.dir, "pages")
	if err := filepath.WalkDir(pagesDir, func(p string, d fs.DirEntry, err error) error {
		if !d.IsDir() {
			return nil
		}

		routePath, err := filepath.Rel(pagesDir, p)
		if err != nil {
			return fmt.Errorf("could not determine relative path for route: %w", err)
		}

		r, err := route.FromDir(p, func(file string) string {
			result := g.assets.GetImageAsset(path.Join(routePath, file))
			if result == "" {
				return file
			}
			return result
		})
		if err != nil {
			return err
		}

		g.routes[path.Join("/", routePath)] = r

		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (g *Generator) Generate() error {
	if err := g.genHandler(); err != nil {
		return fmt.Errorf("could not generate handler: %w", err)
	}

	for rPath, r := range g.routes {
		if err := g.genRoute(rPath, r); err != nil {
			return fmt.Errorf("could not generate route %s: %w", rPath, err)
		}
	}

	return nil
}

func (g *Generator) getRoutesPaths() []string {
	paths := make([]string, 0, len(g.routes))
	for p := range g.routes {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	return paths
}

func (g *Generator) genHandler() error {
	buf := gobuf.New()

	buf.WriteStringLn("package pages")

	buf.WriteStringLn("import (")
	buf.WriteQuotedString("net/http", "\n\n")
	buf.WriteQuotedString("github.com/sergei-svistunov/go-ssr/pkg/mux", "\n\n")
	for _, rPath := range g.getRoutesPaths() {
		if rPath == "/" {
			continue
		}
		buf.WriteString(getRoutePackageAlias(rPath))
		buf.WriteQuotedString(path.Join(g.pkgName, "pages", rPath), "\n")
	}

	buf.WriteStringLn(")")

	// type DataProvider
	buf.WriteStringLn("type DataProvider interface {")
	for _, rPath := range g.getRoutesPaths() {
		variables := g.routes[rPath].Template().GetVariables()
		if len(variables) > 0 {
			if rPath != "/" {
				buf.WriteString(getRoutePackageAlias(rPath))
				buf.WriteString(".")
			}
			buf.WriteStringLn("RouteDataProvider")
		}
	}
	buf.WriteStringLn("}")

	// func NewSsrHandler
	buf.WriteStringLn("func NewSsrHandler(dp DataProvider, opts mux.Options) http.Handler {")
	buf.WriteStringLn("return mux.New(dp, map[string]mux.Route[DataProvider]{")
	for _, rPath := range g.getRoutesPaths() {
		buf.WriteQuotedString(rPath)
		buf.WriteString(": ")
		if rPath != "/" {
			buf.WriteString(getRoutePackageAlias(rPath))
			buf.WriteString(".")
		}
		buf.WriteStringLn("Route[DataProvider]{},")
	}

	buf.WriteStringLn("}, opts)")
	buf.WriteStringLn("}")

	formattedCode, err := buf.Formatted()
	if err != nil {
		return fmt.Errorf("could not format generated code: %w", err)
	}

	if err := os.WriteFile(filepath.Join(g.dir, "pages/ssrhandler.go"), formattedCode, 0755); err != nil {
		return fmt.Errorf("could not write handler.go: %w", err)
	}

	return nil
}

func (g *Generator) genRoute(rPath string, r *route.Route) error {
	routeDir := path.Join(g.pkgName, "pages", rPath)
	buf := gobuf.New()

	buf.WriteString("package ")
	buf.WriteStringLn(filepath.Base(routeDir))

	buf.WriteStringLn("import (")
	buf.WriteQuotedString("context", "\n")
	buf.WriteQuotedString("io", "\n\n")
	buf.WriteQuotedString("github.com/sergei-svistunov/go-ssr/pkg/mux", "\n")
	buf.WriteStringLn(")")

	routeVariables := r.Template().GetVariables()
	// type RouteData
	if len(routeVariables) > 0 {
		buf.WriteStringLn("type RouteData struct {")
		for _, variable := range routeVariables {
			buf.WriteString(getExportedName(variable.Name))
			buf.WriteString(" ")
			buf.WriteStringLn(variable.Type)
		}
		buf.WriteStringLn("}")
		buf.WriteString("\n")
	}

	// RouteDataProvider
	buf.WriteStringLn("type RouteDataProvider interface{")
	if len(routeVariables) > 0 {
		buf.WriteString(getRouteDataProviderMethod(rPath))
		buf.WriteStringLn("(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error")
	}

	if r.Template().GetContentNode() != nil && r.Template().GetContentNode().Default == "" {
		buf.WriteString(getRouteDefaultSubroute(rPath))
		buf.WriteStringLn("(ctx context.Context, r *mux.Request) (string, error)")
	}
	buf.WriteStringLn("}\n")

	// Route[DataProvider RouteRataProvider]
	buf.WriteStringLn("type Route[DataProvider RouteDataProvider] struct {}")

	// func (Route[DataProvider]) GetDataContext
	buf.WriteStringLn("func (Route[DataProvider]) GetDataContext(ctx context.Context, r *mux.Request, w mux.ResponseWriter, dp DataProvider, child mux.DataContext) (mux.DataContext, error) {")
	buf.WriteStringLn("var (")
	buf.WriteStringLn("dataCtx = &dataContext{RouteDataContext: mux.RouteDataContext{")
	buf.WriteStringLn("	Child:  child,")
	buf.WriteStringLn("	Assets: []string{")
	for _, asset := range g.assets.GetTags(rPath) {
		buf.WriteQuotedString(asset, ",\n")
	}
	buf.WriteStringLn("	},")
	buf.WriteStringLn("}}")
	buf.WriteStringLn(")")

	if len(routeVariables) > 0 {
		buf.WriteString("if err := dp.")
		buf.WriteString(getRouteDataProviderMethod(rPath))
		buf.WriteStringLn("(ctx, r, w, &dataCtx.RouteData); err != nil {")
		buf.WriteStringLn("return nil, err")
		buf.WriteStringLn("}")
	}
	buf.WriteStringLn("return dataCtx, nil")
	buf.WriteStringLn("}")
	buf.WriteString("\n")

	// func (r Route[DataProvider]) GetDefaultSubRoute
	buf.WriteString("func (Route[DataProvider]) GetDefaultSubRoute(ctx context.Context, r *mux.Request, dp DataProvider) (string, error) { return ")
	if nodeContent := r.Template().GetContentNode(); nodeContent != nil {
		if nodeContent.Default == "" {
			buf.WriteString("dp.")
			buf.WriteString(getRouteDefaultSubroute(rPath))
			buf.WriteString("(ctx, r)")
		} else {
			buf.WriteQuotedString(nodeContent.Default)
			buf.WriteString(", nil")
		}
	} else {
		buf.WriteString(`"", nil`)
	}

	buf.WriteStringLn("}")

	// type dataContext
	buf.WriteStringLn("type dataContext struct {")
	buf.WriteStringLn("mux.RouteDataContext")
	if len(routeVariables) > 0 {
		buf.WriteStringLn("RouteData")
	}
	buf.WriteStringLn("}")

	// func (c *dataContext) Write
	buf.WriteStringLn("func (c *dataContext) Write(w io.Writer) error {")
	for _, variable := range routeVariables {
		buf.WriteString(variable.Name)
		buf.WriteString(":=c.RouteData.")
		buf.WriteStringLn(getExportedName(variable.Name))
	}
	r.Template().WriteGoCode(buf)
	buf.WriteStringLn("return nil")
	buf.WriteStringLn("}")

	buf.WriteVars()

	formattedCode, err := buf.Formatted()
	if err != nil {
		return fmt.Errorf("could not format generated code: %w\n%s", err, buf.String())
	}

	if err := os.WriteFile(filepath.Join(g.dir, "pages", rPath, "ssrroute.go"), formattedCode, 0755); err != nil {
		return fmt.Errorf("could not write route.go: %w", err)
	}

	return nil
}

func pathToVariable(path string) string {
	if path == "" || path == "/" {
		return "_"
	}

	var res string

	for _, segment := range strings.Split(path, "/") {
		if segment == "" {
			continue
		}

		res += strings.ToUpper(string(segment[0])) + segment[1:]
	}

	return res
}

func getRouteDataProviderMethod(rPath string) string {
	if rPath == "" || rPath == "/" {
		return "GetRouteRootData"
	}

	return "GetRoute" + pathToVariable(rPath) + "Data"
}

func getRouteDefaultSubroute(rPath string) string {
	if rPath == "" || rPath == "/" {
		return "GetRouteDefaultSubRoute"
	}

	return "GetRoute" + pathToVariable(rPath) + "DefaultSubRoute"
}

func getRoutePackageAlias(rPath string) string {
	return "route" + pathToVariable(rPath)
}

func getExportedName(name string) string {
	if name == "" {
		return ""
	}
	return strings.ToUpper(string(name[0])) + name[1:]
}
