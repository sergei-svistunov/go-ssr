// Package docs holds the GoSSR documentation corpus.
//
// The Markdown files are embedded into the go-ssr binary so that the
// documentation served to a tool always describes the generator sitting next to
// it, instead of whatever version happens to be published on the web.
package docs

import "embed"

//go:embed *.md recipes/*.md
var FS embed.FS
