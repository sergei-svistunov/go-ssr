package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect runs the real server over an in-memory transport, so the tests below
// exercise tool registration, schemas and resources the way a host would.
func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	server := newServer()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return session
}

func TestServer_ListsEveryTool(t *testing.T) {
	session := connect(t)

	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	got := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		got[tool.Name] = tool
	}

	want := []string{
		"gossr_docs", "gossr_search_docs", "gossr_init", "gossr_scaffold_route",
		"gossr_generate", "gossr_validate", "gossr_routes", "gossr_webpack",
		"gossr_run", "gossr_logs", "gossr_stop",
	}
	for _, name := range want {
		tool, ok := got[name]
		if !ok {
			t.Fatalf("tool %s not registered; got %v", name, res.Tools)
		}
		if tool.Description == "" {
			t.Errorf("tool %s has no description", name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %s has no input schema", name)
		}
	}
	if len(got) != len(want) {
		t.Errorf("registered %d tools, expected %d", len(got), len(want))
	}

	// Read-only tools must say so: a host may use the hint to skip a prompt.
	for _, name := range []string{"gossr_docs", "gossr_search_docs", "gossr_validate", "gossr_routes", "gossr_logs"} {
		if a := got[name].Annotations; a == nil || !a.ReadOnlyHint {
			t.Errorf("tool %s should be annotated read-only", name)
		}
	}
	for _, name := range []string{"gossr_generate", "gossr_scaffold_route", "gossr_init", "gossr_webpack", "gossr_run", "gossr_stop"} {
		a := got[name].Annotations
		if a == nil || a.ReadOnlyHint {
			t.Errorf("tool %s must not be annotated read-only", name)
		}
		if a != nil && a.DestructiveHint != nil && *a.DestructiveHint {
			t.Errorf("tool %s should not be annotated destructive", name)
		}
	}
}

func TestServer_DocsToolOverTheWire(t *testing.T) {
	session := connect(t)
	ctx := context.Background()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "gossr_docs",
		Arguments: map[string]any{"topic": "overview"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("call failed: %v", res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatal("no content returned")
	}
	body := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(body, "GoSSR") {
		t.Errorf("unexpected content: %.120s", body)
	}
	if res.StructuredContent == nil {
		t.Error("no structured content returned")
	}

	// The schema enum rejects a bad slug before the handler sees it.
	res, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "gossr_docs",
		Arguments: map[string]any{"topic": "not-a-topic"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Error("an unknown topic should be an error result")
	}
}

func TestServer_EveryTopicIsReadableAsAResource(t *testing.T) {
	session := connect(t)
	ctx := context.Background()

	res, err := session.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(res.Resources) != len(topics()) {
		t.Fatalf("%d resources for %d topics", len(res.Resources), len(topics()))
	}

	for _, r := range res.Resources {
		if r.MIMEType != "text/markdown" {
			t.Errorf("resource %s has MIME type %q", r.URI, r.MIMEType)
		}
		if r.Description == "" {
			t.Errorf("resource %s has no description", r.URI)
		}

		read, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: r.URI})
		if err != nil {
			t.Fatalf("ReadResource %s: %v", r.URI, err)
		}
		if len(read.Contents) != 1 || !strings.HasPrefix(read.Contents[0].Text, "# ") {
			t.Errorf("resource %s did not return a Markdown document", r.URI)
		}
	}

	if _, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "gossr://docs/nope"}); err == nil {
		t.Error("reading an unknown resource should fail")
	}
}

func TestServer_ProvidesInstructions(t *testing.T) {
	session := connect(t)

	got := session.InitializeResult().Instructions
	if !strings.Contains(got, "gossr_docs") {
		t.Errorf("instructions should point at the documentation tools: %q", got)
	}
}
