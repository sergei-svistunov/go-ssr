// Package mcp serves GoSSR's documentation and project tools over the Model
// Context Protocol, so an agent can learn the framework and drive the generator
// through one connection.
package mcp

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version is reported to the client. It is a var so a build can stamp it.
var Version = "dev"

// Serve runs the MCP server on stdin and stdout until the client disconnects or
// the context is cancelled.
func Serve(ctx context.Context) error {
	// stdout carries the protocol, so nothing else may write to it. Keeping the
	// original *os.File and pointing the os.Stdout variable at stderr means a
	// stray print anywhere in this process lands in the host's log instead of
	// corrupting the stream. It does not redirect file descriptor 1 itself, so a
	// subprocess still has to be given its own streams — which is what
	// Generator.SetOutput does for every command the generator runs.
	protocolOut := os.Stdout
	protocolIn := os.Stdin
	os.Stdout = os.Stderr

	server := newServer()

	// An application started through gossr_run is a child of this process. Left
	// behind, it would hold its port with no tool able to reach it again.
	defer stopAllApps()

	transport := &mcp.IOTransport{Reader: protocolIn, Writer: protocolOut}
	if err := server.Run(ctx, transport); err != nil {
		return fmt.Errorf("mcp server: %w", err)
	}
	return nil
}

// newServer builds the server with every tool and resource registered. Tests use
// it with an in-memory transport.
func newServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "gossr",
		Title:   "GoSSR",
		Version: Version,
	}, &mcp.ServerOptions{
		Instructions: instructions,
	})

	registerTools(server)
	registerDocResources(server)

	return server
}

const instructions = `GoSSR generates Go HTTP handlers from HTML templates in a directory-based route tree.

It has its own template language (ssr: tags and {{ }} expressions), its own form lifecycle, and a
reactive WebSocket layer. Read the documentation before writing templates or data providers rather
than assuming conventions from other frameworks: start with gossr_docs topic=overview, and use
gossr_search_docs to look up a specific tag, attribute or error message.

Typical loop when changing an app: gossr_routes to see what exists, edit index.html and
dataprovider.go with your own file tools, gossr_generate to regenerate, and gossr_validate if you
only want to check. Generated files (ssrroute_gen.go, ssrhandler_gen.go, ssrstaticfiles_gen.go,
__ssr_gen__.ts) must not be edited by hand.

Generation writes and formats Go code but never compiles it, so a clean gossr_generate does not
mean the application builds. Use gossr_run to start it and find out; it returns the address to
request and keeps running afterwards. gossr_logs shows what it has printed since the last call —
that is where a failed request explains itself — and gossr_stop ends it. Stop the application
before changing generated code, then run it again.`

// registerDocResources publishes every documentation topic as a resource, so
// hosts that browse or attach resources can reach the same corpus the doc tools
// serve.
func registerDocResources(s *mcp.Server) {
	for _, t := range topics() {
		s.AddResource(&mcp.Resource{
			URI:         resourceURI(t.Slug),
			Name:        t.Slug,
			Title:       docTitle(t),
			Description: t.Summary,
			MIMEType:    "text/markdown",
		}, readDocResource)
	}
}

func readDocResource(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	topic, err := findTopic(slugFromURI(req.Params.URI))
	if err != nil {
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     topic.Body,
		}},
	}, nil
}
