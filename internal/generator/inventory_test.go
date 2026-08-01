package generator_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/sergei-svistunov/go-ssr/internal/generator"
)

func TestInventory_DescribesRoutesFormsAndVariables(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, ".", `<html><body><ssr:content default="/users"/></body></html>`)
	writeTemplate(t, webDir, "users", `<div>
<ssr:var name="count" type="int" reactive="true" client-writable="true"/>
<ssr:form name="addUser">
  <ssr:input name="email"/>
  <ssr:input name="age" gotype="int" required/>
  <ssr:textarea name="bio"/>
</ssr:form>
</div>`)
	writeTemplate(t, webDir, filepath.Join("users", "_userId_"), `<p>user</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	inv, err := g.Inventory("")
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}

	if len(inv.Routes) != 3 {
		t.Fatalf("got %d routes, want 3", len(inv.Routes))
	}

	byPath := map[string]generator.RouteInfo{}
	for _, r := range inv.Routes {
		byPath[r.Path] = r
	}

	root := byPath["/"]
	if !root.HasContentSlot {
		t.Error("root route: HasContentSlot = false, want true")
	}
	if root.ContentDefault != "/users" {
		t.Errorf("root route: ContentDefault = %q, want /users", root.ContentDefault)
	}
	// A content slot with an explicit default does not require DefaultRoute.
	if slices.Contains(root.DataProviderMethods, "DefaultRoute") {
		t.Errorf("root route: DataProviderMethods = %v, should not require DefaultRoute", root.DataProviderMethods)
	}

	users := byPath["/users"]
	if !users.Reactive {
		t.Error("/users: Reactive = false, want true")
	}
	if len(users.Variables) != 1 || users.Variables[0].Name != "count" || !users.Variables[0].ClientWritable {
		t.Errorf("/users: Variables = %+v, want one client-writable count", users.Variables)
	}
	if len(users.Forms) != 1 {
		t.Fatalf("/users: got %d forms, want 1", len(users.Forms))
	}
	form := users.Forms[0]
	if form.InitMethod != "InitAddUser" || form.ProcessMethod != "ProcessAddUser" {
		t.Errorf("/users: form hooks = %s/%s, want InitAddUser/ProcessAddUser", form.InitMethod, form.ProcessMethod)
	}
	if len(form.Fields) != 3 {
		t.Fatalf("/users: got %d form fields, want 3: %+v", len(form.Fields), form.Fields)
	}
	fieldTypes := map[string]string{}
	for _, f := range form.Fields {
		fieldTypes[f.Name] = f.GoType + "/" + f.Kind
	}
	if fieldTypes["age"] != "int/input" {
		t.Errorf("age field = %q, want int/input", fieldTypes["age"])
	}
	if fieldTypes["bio"] != "string/textarea" {
		t.Errorf("bio field = %q, want string/textarea", fieldTypes["bio"])
	}

	for _, want := range []string{"Data", "InitAddUser", "ProcessAddUser", "Subscribe", "ValidateCount"} {
		if !slices.Contains(users.DataProviderMethods, want) {
			t.Errorf("/users: DataProviderMethods = %v, missing %s", users.DataProviderMethods, want)
		}
	}

	param := byPath["/users/_userId_"]
	if got := param.URLParams; len(got) != 1 || got[0] != "userId" {
		t.Errorf("/users/_userId_: URLParams = %v, want [userId]", got)
	}

	// The WebSocket endpoint is mounted at the deepest route below a reactive one.
	if len(inv.WSEndpoints) != 1 || inv.WSEndpoints[0] != "/users/_userId_" {
		t.Errorf("WSEndpoints = %v, want [/users/_userId_]", inv.WSEndpoints)
	}
	if param.WSEndpoint != "/users/_userId_/__ws" {
		t.Errorf("/users/_userId_: WSEndpoint = %q, want /users/_userId_/__ws", param.WSEndpoint)
	}
}

func TestInventory_FiltersByRouteAndRejectsUnknown(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, ".", `<html><body><ssr:content/></body></html>`)
	writeTemplate(t, webDir, "users", `<p>users</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	inv, err := g.Inventory("/users")
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	if len(inv.Routes) != 1 || inv.Routes[0].Path != "/users" {
		t.Fatalf("Inventory(/users) = %+v, want only /users", inv.Routes)
	}

	if _, err := g.Inventory("/nope"); err == nil {
		t.Error("Inventory(/nope): expected an error naming the known routes")
	}
}

func TestAssetsStale(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, ".", `<html><body><p>hi</p></body></html>`)
	g := makeGen(t, webDir)

	stale, reason, err := g.AssetsStale()
	if err != nil {
		t.Fatalf("AssetsStale: %v", err)
	}
	if !stale || reason == "" {
		t.Errorf("with no build output: stale = %v, reason = %q, want stale with a reason", stale, reason)
	}

	// A complete build: lock file no older than package.json, assets present and
	// newer than every source.
	writeFile(t, filepath.Join(webDir, "package.json"), "{}")
	writeFile(t, filepath.Join(webDir, "package-lock.json"), "{}")
	writeFile(t, filepath.Join(webDir, "webpack-assets.json"), `{"outputPath":"dist","entrypoints":{},"images":{}}`)
	touch(t, filepath.Join(webDir, "webpack-assets.json"), time.Now().Add(time.Minute))

	stale, reason, err = g.AssetsStale()
	if err != nil {
		t.Fatalf("AssetsStale: %v", err)
	}
	if stale {
		t.Errorf("after a build: stale = true (%s), want false", reason)
	}

	// Editing a stylesheet invalidates the build.
	writeFile(t, filepath.Join(webDir, "pages", "styles.scss"), "body{}")
	touch(t, filepath.Join(webDir, "pages", "styles.scss"), time.Now().Add(2*time.Minute))

	stale, reason, err = g.AssetsStale()
	if err != nil {
		t.Fatalf("AssetsStale: %v", err)
	}
	if !stale {
		t.Error("after editing styles.scss: stale = false, want true")
	}
	if reason == "" {
		t.Error("stale build reported without a reason")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func touch(t *testing.T, path string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}
