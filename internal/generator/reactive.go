package generator

// reactive.go — code-generation helpers for the reactive-bindings feature.
//
// This file contains:
//   - ssrBindScalarTypes set for E07 (ssr:bind on non-scalar variable) check
//   - bindingKey() for assigning <span data-ssr-bind> key values
//   - genReactiveStateCode() for generating per-route ReactiveState types,
//     renderBlock_KEY helpers, updated snapshot(), and Set<VarName> with dep map
//   - genLiveMethods() for making a reactive route's state a mux.Live
//   - genHandleWriteCode() for generating the handleWrite dispatch function
//   - genRouteTSTypes() for writing __ssr_gen__.ts

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
	routepkg "github.com/sergei-svistunov/go-ssr/internal/generator/route"
	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template"
	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/node"
)

// tsTypesFileName is the per-route TypeScript file the generator writes for a
// reactive route. It is generated output living among hand-written sources, so
// anything that inspects the route tree has to know to leave it alone.
const tsTypesFileName = "__ssr_gen__.ts"

// ssrBindScalarTypes is the exhaustive set of Go type-name tokens that are
// accepted as scalar for ssr:bind validation (E07).
//
// The list contains exactly the 17 built-in scalar type tokens accepted for
// ssr:bind. User-defined type aliases (e.g. type UserID string) are NOT in
// this set and are therefore rejected by E07.
//
// complex64 and complex128 are intentionally excluded: encoding/json cannot
// unmarshal any JSON value into a complex type, so a client-writable complex
// variable would produce a decode_error on every write. They are accepted as
// reactive="true" (read-only server-push is fine) but cannot be used with
// ssr:bind (which requires a JSON round-trip for client writes).
var ssrBindScalarTypes = map[string]bool{
	"string":  true,
	"bool":    true,
	"int":     true,
	"int8":    true,
	"int16":   true,
	"int32":   true,
	"int64":   true,
	"uint":    true,
	"uint8":   true,
	"uint16":  true,
	"uint32":  true,
	"uint64":  true,
	"uintptr": true, // numeric platform-width integer; JSON encodes as a number
	"byte":    true,
	"rune":    true,
	"float32": true,
	"float64": true,
}

// bindingKey returns the binding key of a site that reads the reactive
// variables refs and renders the source exprSrc.
//
//   - A site that renders one variable as it is: the variable name (e.g., "counter").
//   - Any other site: lowercase hex SHA-256 of the source, truncated to 16
//     characters (e.g., "a1b2c3d4e5f60708"), so that two sites share a key only
//     when they render the same markup.
func bindingKey(refs []string, exprSrc string) string {
	if len(refs) == 1 && exprSrc == refs[0] {
		return refs[0]
	}
	sum := sha256.Sum256([]byte(exprSrc))
	return fmt.Sprintf("%x", sum[:8]) // 8 bytes = 16 hex chars
}

// reactiveVars returns the subset of template variables that have Reactive=true,
// sorted by name for deterministic output.
func reactiveVars(vars []template.Variable) []template.Variable {
	var rv []template.Variable
	for _, v := range vars {
		if v.Reactive {
			rv = append(rv, v)
		}
	}
	sort.Slice(rv, func(i, j int) bool { return rv[i].Name < rv[j].Name })
	return rv
}

// clientWritableVars returns reactive vars that also have ClientWritable=true.
func clientWritableVars(vars []template.Variable) []template.Variable {
	var cw []template.Variable
	for _, v := range vars {
		if v.Reactive && v.ClientWritable {
			cw = append(cw, v)
		}
	}
	sort.Slice(cw, func(i, j int) bool { return cw[i].Name < cw[j].Name })
	return cw
}

// validateSsrBindE07 checks E07: ssr:bind used on a variable whose Go type is
// not in the scalar type set.
//
// E04 (scalar-only restriction on reactive="true") is retired.
// Any Go type may now be used with reactive="true". E07 restricts only
// ssr:bind, which must reference a scalar variable because native HTML input
// .value is always a string.
//
// varMap is the route's name→Variable map (already built by validateReactiveBindings).
func validateSsrBindE07(rPath string, tmpl *template.Template, varMap map[string]template.Variable) *Diagnostic {
	for _, ref := range tmpl.GetSsrBindRefs() {
		v, ok := varMap[ref.VarName]
		if !ok {
			// Variable not declared in this route — handled by validateSsrBindRefs (E05/cross-route).
			continue
		}
		if !ssrBindScalarTypes[v.Type] {
			return &Diagnostic{
				Route: rPath,
				Line:  ref.Line,
				Code:  "E07",
				Message: fmt.Sprintf("reactive-bindings: ssr:bind=%q requires a scalar variable type; %q has type %q. Use ssr.set() in TypeScript for non-scalar reactive variables.",
					ref.VarName, ref.VarName, v.Type),
			}
		}
	}
	return nil
}

// validateSsrBindRefs checks E05 (ssr:bind without client-writable) and the
// cross-route ssr:bind constraint (no-cross-route rule).
//
// allRouteVarMaps is a map from routePath → varName → Variable for every route
// in the generation run. It is used to provide a better error message when an
// ssr:bind references a variable that exists in a DIFFERENT route.
//
// Three cases are distinguished:
//  1. Variable declared in this route but not client-writable → E05 error.
//  2. Variable not declared in this route but found in a different route →
//     cross-route error (no-cross-route constraint).
//  3. Variable not declared in any route → "variable not declared" error.
//
// It does NOT check E06 here because E06 is detected at parse time and already
// converted to a SsrBindOnPrimitiveError by the template parser.
func validateSsrBindRefs(rPath string, tmpl *template.Template, allRouteVarMaps map[string]map[string]template.Variable) *Diagnostic {
	varMap := make(map[string]template.Variable)
	for _, v := range tmpl.GetVariables() {
		varMap[v.Name] = v
	}
	for _, ref := range tmpl.GetSsrBindRefs() {
		v, ok := varMap[ref.VarName]
		if ok {
			// Variable exists in this route.
			if !v.ClientWritable {
				return &Diagnostic{
					Route: rPath,
					Line:  ref.Line,
					Code:  "E05",
					Message: fmt.Sprintf("reactive-bindings: ssr:bind=%q at route %q requires client-writable=\"true\" on the variable declaration; add client-writable=\"true\" to the <ssr:var> or remove ssr:bind",
						ref.VarName, rPath),
				}
			}
			// client-writable=true — valid.
			continue
		}

		// Variable not in this route. Search other routes.
		declaringRoute := ""
		for otherPath, otherVarMap := range allRouteVarMaps {
			if otherPath == rPath {
				continue
			}
			if _, found := otherVarMap[ref.VarName]; found {
				declaringRoute = otherPath
				break
			}
		}

		if declaringRoute != "" {
			// Found in a different route — cross-route binding is not allowed.
			return &Diagnostic{
				Route: rPath,
				Line:  ref.Line,
				Code:  "E05",
				Message: fmt.Sprintf("reactive-bindings: ssr:bind=%q at route %q references a variable declared in a different route (%q); ssr:bind references must be local to the declaring route",
					ref.VarName, rPath, declaringRoute),
			}
		}

		// Not found anywhere — variable not declared.
		return &Diagnostic{
			Route: rPath,
			Line:  ref.Line,
			Code:  "E05",
			Message: fmt.Sprintf("reactive-bindings: ssr:bind=%q at route %q references variable %q which is not declared in any route; add <ssr:var name=%q .../> to this route's template",
				ref.VarName, rPath, ref.VarName, ref.VarName),
		}
	}
	return nil
}

// routeNeedsStringsBuilder returns true when the route is reactive and will
// emit at least one writeBlock_KEY/renderBlock_KEY helper, each of which uses
// strings.Builder.
//
// This is a pure analysis pass — it does NOT mutate the template AST.
// It must be called before AnnotateBindings so that we can determine import
// requirements before emitting the import block.
func routeNeedsStringsBuilder(tmpl *template.Template, reactiveMap map[string]bool) bool {
	if tmpl == nil || len(reactiveMap) == 0 {
		return false
	}
	// Check each top-level node for reactive SsrCondition or Loop.
	return checkNeedsStrings(tmpl.GetNodes(), reactiveMap)
}

func checkNeedsStrings(nodes []node.Node, reactiveMap map[string]bool) bool {
	for _, n := range nodes {
		if checkNeedsStringsNode(n, reactiveMap) {
			return true
		}
	}
	return false
}

func checkNeedsStringsNode(n node.Node, reactiveMap map[string]bool) bool {
	switch v := n.(type) {
	case *node.Expression:
		// Every site renders through a strings.Builder.
		if len(v.CollectVarRefs(reactiveMap)) > 0 {
			return true
		}
	case *node.RawExpression:
		if len(v.CollectVarRefs(reactiveMap)) > 0 {
			return true
		}
	case *node.SsrCondition:
		refs := v.CollectVarRefs(reactiveMap)
		if len(refs) > 0 {
			// Block site always needs strings.Builder.
			return true
		}
		// Recurse into non-reactive branches.
		for _, c := range v.Conditions {
			if checkNeedsStringsNode(c.Body, reactiveMap) {
				return true
			}
		}
		if v.ElseBody != nil {
			if checkNeedsStringsNode(v.ElseBody, reactiveMap) {
				return true
			}
		}
	case *node.Loop:
		refs := v.CollectVarRefs(reactiveMap)
		if len(refs) > 0 {
			return true
		}
		if checkNeedsStrings(v.Children, reactiveMap) {
			return true
		}
	case *node.HtmlElement:
		// Reactive attribute → element is a block site → strings.Builder needed.
		if len(v.CollectAttributeVarRefs(reactiveMap)) > 0 {
			return true
		}
		if checkNeedsStrings(v.Children, reactiveMap) {
			return true
		}
	case *node.Content:
		if checkNeedsStrings(v.Children, reactiveMap) {
			return true
		}
	}
	return false
}

// genReactiveStateCode appends the ReactiveState struct, renderBlock_KEY
// helpers, Set<VarName> methods (with dep-map dispatch), snapshot(), and
// handleWrite dispatch function to the provided GoBuf.
//
// It also annotates Expression/RawExpression/SsrCondition/Loop nodes in the
// template with their binding keys (mutates the AST in-place so WriteGoCode
// emits the span/ssr-block wrappers). All emitted keys are namespaced with
// the route's routeKey prefix.
//
// Returns the namespaced binding-key → []reactive-var-names dependency map
// (used to emit Set<VarName> dispatch code), or nil if the route is
// non-reactive.
func (g *Generator) genReactiveStateCode(buf *gobuf.GoBuf, rPath string, r *routepkg.Route) (map[string][]string, error) {
	tmpl := r.Template()
	vars := tmpl.GetVariables()
	rv := reactiveVars(vars)

	if len(rv) == 0 {
		return nil, nil
	}

	// Compute the route's key constant (sha256(routePath)[:8]).
	rk := routeKeyFor(rPath)

	// Emit the routeKey constant.
	buf.WriteStringLn("// routeKey is the 8-hex route path hash used to namespace binding keys")
	buf.WriteStringLn("// on the wire. Stable for a given route path.")
	buf.WriteString("const routeKey = ")
	buf.WriteQuotedString(rk)
	buf.WriteStringLn("")
	buf.WriteString("\n")

	// Build reactive map for CollectVarRefs (reactive vars only).
	reactiveMap := make(map[string]bool, len(rv))
	for _, v := range rv {
		reactiveMap[v.Name] = true
	}

	// Build a full var map (all template variables, reactive and non-reactive)
	// for alias emission in writeBlock_* helpers. This allows CollectVarRefs
	// to identify every variable a block actually references, not just reactive ones.
	allVarsMap := make(map[string]bool, len(vars))
	varsByName := make(map[string]template.Variable, len(vars))
	for _, v := range vars {
		allVarsMap[v.Name] = true
		varsByName[v.Name] = v
	}

	// Annotate binding keys on Expression/RawExpression nodes and block keys on
	// SsrCondition/Loop nodes. Keys are namespaced with routeKey prefix.
	bindings := tmpl.AnnotateBindings(reactiveMap, bindingKey, rk)

	// Collect annotated nodes for renderBlock_KEY emission.
	// The nodes are keyed by their (namespaced) BindingKey/BlockKey.
	reactiveNodes := tmpl.CollectReactiveNodes()

	// Build the reverse dependency map: varName → sorted []namespacedKeys.
	// This is used to know which keys to re-render when a variable changes.
	varToKeys := make(map[string][]string)
	for nsKey, refs := range bindings {
		for _, ref := range refs {
			varToKeys[ref] = append(varToKeys[ref], nsKey)
		}
	}
	// Sort the key lists for deterministic output.
	for varName := range varToKeys {
		sort.Strings(varToKeys[varName])
	}

	// Sorted binding keys for deterministic output.
	sortedNsKeys := make([]string, 0, len(bindings))
	for k := range bindings {
		sortedNsKeys = append(sortedNsKeys, k)
	}
	sort.Strings(sortedNsKeys)

	// localKeyOf strips the routeKey prefix from a namespaced key to produce
	// the local key used for Go function names (which cannot contain dots).
	localKeyOf := func(nsKey string) string {
		prefix := rk + "."
		if strings.HasPrefix(nsKey, prefix) {
			return nsKey[len(prefix):]
		}
		return nsKey // no prefix (empty routeKey)
	}

	// ---------------------------------------------------------------------------
	// Emit renderBlock_KEY helper functions.
	// Each helper renders only the specific binding site to a string.
	// Used by snapshot() and Set<VarName> for patch emission.
	//
	// Function names use the LOCAL key (no dot) so they are valid Go identifiers.
	// The namespaced key (with routeKey prefix) is used only in snapshot() and
	// Enqueue() calls.
	//
	// Every site renders through writeBlock_KEY with the escapers of the first
	// render, so a live value is the markup the page wrote for the same site.
	// ---------------------------------------------------------------------------
	for _, nsKey := range sortedNsKeys {
		n, ok := reactiveNodes[nsKey]
		if !ok {
			// Should not happen: every annotated key has a node.
			continue
		}

		localKey := localKeyOf(nsKey)
		buf.WriteStringLn("// writeBlock_" + localKey + " renders binding site \"" + nsKey + "\" into w.")
		buf.WriteStringLn("func writeBlock_" + localKey + "(data *RouteData, w io.Writer) error {")

		// Emit variable aliases for only those template variables that are
		// actually referenced inside this block. CollectVarRefs with the full
		// allVarsMap (reactive + non-reactive) returns every var the block uses,
		// preventing "declared and not used" compile errors when a block references
		// only a subset of the route's variables.
		referencedNames := n.CollectVarRefs(allVarsMap)
		sort.Strings(referencedNames) // deterministic output
		for _, name := range referencedNames {
			v := varsByName[name]
			buf.WriteStringLn(v.FilePos())
			buf.WriteString(v.Name)
			buf.WriteString(":=data.")
			buf.WriteStringLn(getExportedName(v.Name))
		}
		buf.WriteStringLn("_ = w // w io.Writer is used by WriteInnerGoCode; this silences the linter when the block body emits no direct writes")

		// Emit the block/expression body without wrappers.
		switch v := n.(type) {
		case *node.Expression:
			v.WriteInnerGoCode(buf)
		case *node.RawExpression:
			v.WriteInnerGoCode(buf)
		case *node.SsrCondition:
			v.WriteInnerGoCode(buf)
		case *node.Loop:
			v.WriteInnerGoCode(buf)
		case *node.HtmlElement:
			v.WriteInnerGoCode(buf)
		}

		buf.WriteStringLn("return nil")
		buf.WriteStringLn("}")
		buf.WriteString("\n")

		buf.WriteStringLn("// renderBlock_" + localKey + " returns the current rendered HTML for binding key \"" + nsKey + "\".")
		buf.WriteStringLn("func renderBlock_" + localKey + "(data *RouteData) string {")
		buf.WriteStringLn("var __w strings.Builder")
		buf.WriteStringLn("_ = writeBlock_" + localKey + "(data, &__w)")
		buf.WriteStringLn("return __w.String()")
		buf.WriteStringLn("}")
		buf.WriteString("\n")
	}

	// ---------------------------------------------------------------------------
	// type ReactiveState struct
	// ---------------------------------------------------------------------------
	buf.WriteStringLn("// ReactiveState holds the current live values for all reactive variables")
	buf.WriteStringLn("// in this route. Call Set<VarName> to push an update to the connected client.")
	buf.WriteStringLn("type ReactiveState struct {")
	buf.WriteStringLn("conn     *reactive.Conn")
	buf.WriteStringLn("routeKey string // owning route's 8-hex path hash (for patch frames)")
	buf.WriteStringLn("data     *RouteData // current route data; Set* methods update this in-place")
	for _, v := range rv {
		buf.WriteStringLn("mu" + getExportedName(v.Name) + " sync.Mutex")
	}
	buf.WriteStringLn("}")
	buf.WriteString("\n")

	// NewReactiveState constructor (accepts routeKey for patch frame tagging).
	buf.WriteStringLn("func NewReactiveState(conn *reactive.Conn, data *RouteData) *ReactiveState {")
	buf.WriteStringLn("return &ReactiveState{conn: conn, routeKey: routeKey, data: data}")
	buf.WriteStringLn("}")
	buf.WriteString("\n")

	// ---------------------------------------------------------------------------
	// Set<VarName> methods — look up the dep map and enqueue a patch per key.
	// Enqueue receives (routeKey, namespacedKey, html) so PatchMsg carries
	// the correct routeKey field.
	// ---------------------------------------------------------------------------
	for _, v := range rv {
		exportedName := getExportedName(v.Name)
		affectedNsKeys := varToKeys[v.Name] // sorted list of namespaced keys that depend on this var

		buf.WriteStringLn("// Set" + exportedName + " pushes a new value for the " + v.Name + " binding.")
		buf.WriteStringLn("// It is non-blocking and safe to call from any goroutine.")
		buf.WriteStringLn("func (s *ReactiveState) Set" + exportedName + "(v " + v.Type + ") {")
		buf.WriteStringLn("if s.conn.Ctx().Err() != nil { return }")
		buf.WriteStringLn("s.mu" + exportedName + ".Lock()")
		buf.WriteString("s.data.")
		buf.WriteStringLn(exportedName + " = v")
		buf.WriteStringLn("s.mu" + exportedName + ".Unlock()")

		// Enqueue a patch for each binding key that depends on this variable.
		for _, nsKey := range affectedNsKeys {
			localKey := localKeyOf(nsKey)
			buf.WriteString("s.conn.Enqueue(s.routeKey, ")
			buf.WriteQuotedString(nsKey)
			buf.WriteStringLn(", renderBlock_" + localKey + "(s.data))")
		}

		buf.WriteStringLn("}")
		buf.WriteString("\n")
	}

	// ---------------------------------------------------------------------------
	// snapshot function — calls every renderBlock_KEY for a complete init snapshot.
	// Returns a map[namespacedKey]renderedHTML covering all reactive binding sites.
	// ---------------------------------------------------------------------------
	buf.WriteStringLn("// Snapshot returns the current rendered values for all reactive binding sites.")
	buf.WriteStringLn("// It is called to build the init message sent to newly connected clients.")
	buf.WriteStringLn("func Snapshot(data *RouteData) map[string]string {")
	buf.WriteStringLn("m := map[string]string{}")
	for _, nsKey := range sortedNsKeys {
		localKey := localKeyOf(nsKey)
		buf.WriteString("m[")
		buf.WriteQuotedString(nsKey)
		buf.WriteString("] = renderBlock_" + localKey + "(data)")
		buf.WriteStringLn("")
	}
	buf.WriteStringLn("return m")
	buf.WriteStringLn("}")
	buf.WriteString("\n")

	return bindings, nil
}

// genLiveMethods makes a reactive route's state implement mux.Live. The
// ReactiveState is made once per connection, by whichever of Subscribe and
// HandleWrite runs first.
func (g *Generator) genLiveMethods(buf *gobuf.GoBuf) {
	buf.WriteStringLn("var _ mux.Live = (*ssrState)(nil)")
	buf.WriteString("\n")
	buf.WriteStringLn("func (c *ssrState) RouteKey() string { return routeKey }")
	buf.WriteString("\n")
	buf.WriteStringLn("func (c *ssrState) Snapshot() map[string]string { return Snapshot(&c.RouteData) }")
	buf.WriteString("\n")
	buf.WriteStringLn("func (c *ssrState) Subscribe(ctx context.Context, r *mux.Request, conn *reactive.Conn) error {")
	buf.WriteStringLn("return c.dp.Subscribe(ctx, r, c.reactiveState(conn))")
	buf.WriteStringLn("}")
	buf.WriteString("\n")
	buf.WriteStringLn("func (c *ssrState) HandleWrite(ctx context.Context, r *mux.Request, conn *reactive.Conn, msg reactive.WriteMsg) {")
	buf.WriteStringLn("HandleWrite(ctx, r, conn, c.dp, c.reactiveState(conn), &msg)")
	buf.WriteStringLn("}")
	buf.WriteString("\n")
	buf.WriteStringLn("func (c *ssrState) reactiveState(conn *reactive.Conn) *ReactiveState {")
	buf.WriteStringLn("c.liveOnce.Do(func() { c.live = NewReactiveState(conn, &c.RouteData) })")
	buf.WriteStringLn("return c.live")
	buf.WriteStringLn("}")
	buf.WriteString("\n")
}

// genHandleWriteCode generates the handleWrite function for client→server write
// message dispatch. It is emitted into the route's ssrroute_gen.go.
// The function echoes msg.RouteKey in ack/err frames.
//
// The write frame's Value field is json.RawMessage. The dispatch uses
// json.Unmarshal to decode the value into the target Go type. On unmarshal
// failure the server returns err{code:"decode_error"} and does not modify the variable.
func (g *Generator) genHandleWriteCode(buf *gobuf.GoBuf, r *routepkg.Route) {
	cv := clientWritableVars(r.Template().GetVariables())

	buf.WriteStringLn("// HandleWrite processes a client→server write message for this route.")
	buf.WriteStringLn("// It validates the value and, if valid, updates the ReactiveState.")
	buf.WriteStringLn("// msg.RouteKey is echoed back in ack/err frames.")
	buf.WriteStringLn("func HandleWrite(ctx context.Context, r *mux.Request, conn *reactive.Conn, dp RouteDataProvider, state *ReactiveState, msg *reactive.WriteMsg) {")
	buf.WriteStringLn("switch msg.Var {")
	for _, v := range cv {
		exportedName := getExportedName(v.Name)
		buf.WriteString("case ")
		buf.WriteQuotedString(v.Name)
		buf.WriteStringLn(":")
		// JSON-decode the raw message value into the target Go type.
		buf.WriteStringLn("{")
		buf.WriteStringLn("var val " + v.Type)
		buf.WriteStringLn("if err := json.Unmarshal(msg.Value, &val); err != nil {")
		parse, refuse, assign, refusal := textParse(v.Type)
		if parse != "" {
			// An ssr:bind input writes its text, a JSON string.
			buf.WriteStringLn("var s string")
			buf.WriteStringLn("if json.Unmarshal(msg.Value, &s) != nil {")
		}
		buf.WriteStringLn("conn.SendJSON(reactive.NewErrMsg(msg.RouteKey, msg.Var, \"decode error: \"+err.Error(), \"decode_error\"))")
		buf.WriteStringLn("return")
		buf.WriteStringLn("}")
		if parse != "" {
			buf.WriteStringLn(parse)
			buf.WriteStringLn("if " + refuse + " {")
			buf.WriteStringLn("conn.SendJSON(reactive.NewErrMsg(msg.RouteKey, msg.Var, " + strconv.Quote(refusal) + ", \"validation_failed\"))")
			buf.WriteStringLn("return")
			buf.WriteStringLn("}")
			buf.WriteStringLn(assign)
			buf.WriteStringLn("}")
		}
		buf.WriteStringLn("validated, err := dp.Validate" + exportedName + "(ctx, r, val)")
		buf.WriteStringLn("if err != nil {")
		buf.WriteStringLn("conn.SendJSON(reactive.NewErrMsg(msg.RouteKey, msg.Var, err.Error(), \"validation_failed\"))")
		buf.WriteStringLn("return")
		buf.WriteStringLn("}")
		buf.WriteStringLn("conn.SendJSON(reactive.NewAckMsg(msg.RouteKey, msg.Var))")
		buf.WriteStringLn("state.Set" + exportedName + "(validated)")
		buf.WriteStringLn("}")
	}
	buf.WriteStringLn("default:")
	buf.WriteStringLn("conn.SendJSON(reactive.NewErrMsg(msg.RouteKey, msg.Var, \"unknown variable\", \"validation_failed\"))")
	buf.WriteStringLn("}")
	buf.WriteStringLn("}")
	buf.WriteString("\n")
}

// textParse returns how the generated code reads s, the text of an ssr:bind
// input, into val of the scalar type t: the statement that parses s, the
// condition that refuses it, the assignment to val, and the message of the
// refusal. All are "" for a type encoding/json reads from a JSON string itself.
func textParse(t string) (parse, refuse, assign, refusal string) {
	bits := map[string]string{
		"int": "0", "int8": "8", "int16": "16", "int32": "32", "rune": "32", "int64": "64",
		"uint": "0", "uintptr": "0", "uint8": "8", "byte": "8", "uint16": "16", "uint32": "32", "uint64": "64",
		"float32": "32", "float64": "64",
	}[t]
	switch {
	case t == "bool":
		return "b, err := strconv.ParseBool(s)", "err != nil", "val = b", "not true or false"
	case strings.HasPrefix(t, "float"):
		return "f, err := strconv.ParseFloat(s, " + bits + ")", "err != nil || math.IsNaN(f) || math.IsInf(f, 0)", "val = " + t + "(f)", "not a number"
	case strings.HasPrefix(t, "int") || t == "rune":
		return "n, err := strconv.ParseInt(s, 10, " + bits + ")", "err != nil", "val = " + t + "(n)", "not a whole number"
	case strings.HasPrefix(t, "uint") || t == "byte":
		return "n, err := strconv.ParseUint(s, 10, " + bits + ")", "err != nil", "val = " + t + "(n)", "not a whole number"
	}
	return "", "", "", ""
}

// isReactiveRoute returns true if the route has at least one reactive variable.
func isReactiveRoute(r *routepkg.Route) bool {
	if r.Template() == nil {
		return false
	}
	for _, v := range r.Template().GetVariables() {
		if v.Reactive {
			return true
		}
	}
	return false
}

// routeKeyFor computes the 8-hex-character route key for a given route path.
// It is deterministic: sha256(routePath)[:4] encoded as lowercase hex.
func routeKeyFor(rPath string) string {
	sum := sha256.Sum256([]byte(rPath))
	return fmt.Sprintf("%x", sum[:4]) // 4 bytes = 8 hex chars
}

// genRouteTSTypes writes __ssr_gen__.ts next to the index.html of a route with
// live values or calls: the TypeScript types of its client-writable variables
// and its calls' arguments and results, mapped from the route package's declarations
// as encoding/json writes them, and its typed client. A type the mapper cannot
// name exactly becomes unknown, with a warning.
func (g *Generator) genRouteTSTypes(rPath string, r *routepkg.Route) error {
	if !hasLiveConnection(r) {
		return nil
	}

	indexPath := g.routeIndexPath(rPath)
	m := newTSMapper(g, loadGoPackage(filepath.Dir(indexPath)))
	vars := r.Template().GetVariables()
	writeTS := map[string]string{}
	for _, v := range clientWritableVars(vars) {
		writeTS[v.Name] = m.typeOf(v.Type, indexPath, v.Line)
	}
	calls := r.Template().GetCalls()
	callTS := make([][2]string, len(calls))
	for i, c := range calls {
		callTS[i] = [2]string{m.typeOf(c.In, indexPath, c.Line), m.typeOf(c.Out, indexPath, c.Line)}
	}

	var sb strings.Builder
	sb.WriteString("// Code generated by github.com/sergei-svistunov/go-ssr. DO NOT EDIT.\n")
	sb.WriteString("// Typed reactive client for route ")
	sb.WriteString(rPath)
	sb.WriteString("\n\n")
	sb.WriteString("import { createSsrClient } from 'gossr-runtime';\n")
	sb.WriteString("import 'gossr-runtime/ssr-block.css';\n\n")

	for _, decl := range m.declarations() {
		sb.WriteString(decl)
		sb.WriteString("\n\n")
	}

	// ReadVars: every reactive variable, for ssr.get and ssr.on, which give the
	// markup the server rendered for it.
	sb.WriteString("export type ReadVars = {\n")
	for _, v := range reactiveVars(vars) {
		sb.WriteString("  " + v.Name + ": string;\n")
	}
	sb.WriteString("}\n\n")

	// WriteVars: the client-writable variables, for ssr.set, which sends a
	// JSON value.
	sb.WriteString("export type WriteVars = {\n")
	for _, v := range clientWritableVars(vars) {
		sb.WriteString("  " + v.Name + ": " + writeTS[v.Name] + ";\n")
	}
	sb.WriteString("}\n\n")

	// Calls: each call's argument and result, for ssr.call. A route without
	// calls passes an empty type, so no name type-checks.
	if len(calls) == 0 {
		sb.WriteString("export type Calls = {};\n\n")
	} else {
		sb.WriteString("export type Calls = {\n")
		for i, c := range calls {
			sb.WriteString("  " + c.Name + ": { in: " + callTS[i][0] + "; out: " + callTS[i][1] + " };\n")
		}
		sb.WriteString("}\n\n")
	}

	// The client's wsUrl is computed at runtime from the page's own address,
	// query included, so the live connection's hooks see what the page's hooks
	// saw.
	sb.WriteString("export const ssr = createSsrClient<ReadVars, WriteVars, Calls>({\n")
	sb.WriteString("  wsUrl: typeof window !== 'undefined'\n")
	sb.WriteString("    ? window.location.pathname.replace(/\\/$/, '') + '/__ws' + window.location.search\n")
	sb.WriteString("    : '',\n")
	sb.WriteString("  routeKey: '")
	sb.WriteString(routeKeyFor(rPath))
	sb.WriteString("',\n")
	sb.WriteString("});\n")

	outPath := filepath.Join(g.webDir, "pages", rPath, tsTypesFileName)
	if err := os.WriteFile(outPath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("could not write %s: %w", outPath, err)
	}

	return nil
}
