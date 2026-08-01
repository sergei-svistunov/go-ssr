package mcp

import (
	"fmt"
	"path"
	"strings"
)

// starterTemplate is the smallest template that is a valid route and shows the
// conventions a caller is expected to build on: a declared variable with an
// explicit Go type, and a content slot when the route is a layout.
func starterTemplate(routePath, heading string, hasChildren bool) string {
	if heading == "" {
		heading = defaultHeading(routePath)
	}

	var b strings.Builder
	b.WriteString("<ssr:var name=\"title\" type=\"string\"/>\n")
	fmt.Fprintf(&b, "<h1>{{ title }}</h1>\n")
	fmt.Fprintf(&b, "<p>%s</p>\n", heading)
	if hasChildren {
		b.WriteString("\n<!-- Child routes render here. Add default=\"/child\" or implement DefaultRoute. -->\n")
		b.WriteString("<ssr:content/>\n")
	}
	return b.String()
}

func starterTypeScript(routePath string) string {
	return fmt.Sprintf("// Client-side behaviour for %s.\n"+
		"// Bundled into this route's chunk automatically; no registration needed.\n"+
		"export {};\n", routePath)
}

func starterStyles(routePath string) string {
	return fmt.Sprintf("/* Styles for %s. Bundled into this route's chunk automatically. */\n", routePath)
}

func defaultHeading(routePath string) string {
	base := path.Base(routePath)
	if strings.HasPrefix(base, "_") && strings.HasSuffix(base, "_") {
		return "Details for {{ title }}."
	}
	return "Replace this with the content of the " + base + " page."
}
