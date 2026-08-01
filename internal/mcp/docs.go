package mcp

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/sergei-svistunov/go-ssr/docs"
)

// Topic is one documentation file from the embedded corpus.
type Topic struct {
	Slug    string
	Title   string
	Summary string
	Body    string
}

// topics returns the corpus index, parsed once. The files are embedded, so a
// malformed corpus is a build-time mistake rather than a runtime condition; a
// missing title degrades to the slug instead of failing the server.
var topics = sync.OnceValue(func() []Topic {
	var out []Topic

	_ = fs.WalkDir(docs.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		body, err := fs.ReadFile(docs.FS, p)
		if err != nil {
			return nil
		}
		out = append(out, parseTopic(strings.TrimSuffix(p, ".md"), string(body)))
		return nil
	})

	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
})

var topicIndex = sync.OnceValue(func() map[string]Topic {
	m := make(map[string]Topic)
	for _, t := range topics() {
		m[t.Slug] = t
	}
	return m
})

func parseTopic(slug, body string) Topic {
	t := Topic{Slug: slug, Title: slug, Body: body}

	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "# ") {
			continue
		}
		t.Title = strings.TrimSpace(strings.TrimPrefix(line, "# "))

		// The summary is the first paragraph after the title.
		var summary []string
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) == "" {
				if len(summary) > 0 {
					break
				}
				continue
			}
			if strings.HasPrefix(next, "#") {
				break
			}
			summary = append(summary, strings.TrimSpace(next))
		}
		t.Summary = strings.Join(summary, " ")
		break
	}

	return t
}

// topicSlugs lists every slug, for the tool schema's enum.
func topicSlugs() []string {
	all := topics()
	slugs := make([]string, len(all))
	for i, t := range all {
		slugs[i] = t.Slug
	}
	return slugs
}

// topicList renders the index as Markdown, so the tool description can carry the
// whole map of the corpus and save a discovery round trip.
func topicList() string {
	var b strings.Builder
	for _, t := range topics() {
		fmt.Fprintf(&b, "- `%s` — %s\n", t.Slug, t.Title)
	}
	return b.String()
}

func findTopic(slug string) (Topic, error) {
	slug = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(slug), "/"), ".md")
	if t, ok := topicIndex()[slug]; ok {
		return t, nil
	}
	return Topic{}, fmt.Errorf("unknown documentation topic %q; available topics: %s",
		slug, strings.Join(topicSlugs(), ", "))
}

// section extracts a single "## " section of a topic, including any deeper
// headings nested under it. An empty name returns the whole body.
func section(t Topic, name string) (string, error) {
	if name == "" {
		return t.Body, nil
	}

	want := strings.ToLower(strings.TrimSpace(name))
	lines := strings.Split(t.Body, "\n")
	levels := headingLevels(lines)

	start, depth := -1, 0
	for i, line := range lines {
		level := levels[i]
		if level == 0 {
			continue
		}
		heading := strings.ToLower(strings.TrimSpace(strings.TrimLeft(line, "# ")))
		if start < 0 {
			if level >= 2 && (heading == want || strings.Contains(heading, want)) {
				start, depth = i, level
			}
			continue
		}
		if level <= depth {
			return strings.Join(lines[start:i], "\n"), nil
		}
	}

	if start < 0 {
		return "", fmt.Errorf("topic %q has no section matching %q; sections: %s",
			t.Slug, name, strings.Join(sectionNames(t), ", "))
	}
	return strings.Join(lines[start:], "\n"), nil
}

func sectionNames(t Topic) []string {
	lines := strings.Split(t.Body, "\n")
	levels := headingLevels(lines)

	var names []string
	for i, line := range lines {
		if levels[i] == 2 {
			names = append(names, strings.TrimSpace(strings.TrimLeft(line, "# ")))
		}
	}
	return names
}

// headingLevels scores every line of a document: the number of leading '#' for a
// heading, zero for anything else. Lines inside a fenced code block are never
// headings — a shell comment such as "# scaffold" would otherwise cut a section
// short and hand a caller a truncated, unclosed code block.
func headingLevels(lines []string) []int {
	levels := make([]int, len(lines))
	fence := ""

	for i, line := range lines {
		if f := fenceMarker(line); f != "" {
			switch {
			case fence == "":
				fence = f
			case strings.HasPrefix(f, fence):
				fence = ""
			}
			continue
		}
		if fence == "" {
			levels[i] = headingLevel(line)
		}
	}

	return levels
}

// fenceMarker returns the backtick or tilde run that opens or closes a fenced
// code block, or an empty string for an ordinary line.
func fenceMarker(line string) string {
	t := strings.TrimLeft(line, " ")
	for _, c := range []byte{'`', '~'} {
		n := 0
		for n < len(t) && t[n] == c {
			n++
		}
		if n >= 3 {
			return t[:n]
		}
	}
	return ""
}

func headingLevel(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n >= len(line) || line[n] != ' ' {
		return 0
	}
	return n
}

// SearchHit is one matching section of one topic.
type SearchHit struct {
	Topic   string `json:"topic"`
	Title   string `json:"title"`
	Section string `json:"section,omitempty"`
	Snippet string `json:"snippet"`
}

// searchDocs matches the query case-insensitively against headings and body
// text, reporting the enclosing section so a caller can fetch it directly.
func searchDocs(query string, limit int) []SearchHit {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}

	var hits []SearchHit

	for _, t := range topics() {
		lines := strings.Split(t.Body, "\n")
		levels := headingLevels(lines)
		curSection := ""
		matchedSections := map[string]bool{}

		for i, line := range lines {
			if levels[i] >= 2 {
				curSection = strings.TrimSpace(strings.TrimLeft(line, "# "))
			}
			if !strings.Contains(strings.ToLower(line), q) {
				continue
			}
			// One hit per section keeps the result readable when a term appears
			// many times in the same passage.
			key := t.Slug + "#" + curSection
			if matchedSections[key] {
				continue
			}
			matchedSections[key] = true

			hits = append(hits, SearchHit{
				Topic:   t.Slug,
				Title:   t.Title,
				Section: curSection,
				Snippet: snippet(lines, i),
			})
			if len(hits) >= limit {
				return hits
			}
		}
	}

	return hits
}

func snippet(lines []string, i int) string {
	from := max(i-1, 0)
	to := min(i+2, len(lines))

	var kept []string
	for _, l := range lines[from:to] {
		if strings.TrimSpace(l) != "" {
			kept = append(kept, strings.TrimSpace(l))
		}
	}
	return strings.Join(kept, " ")
}

// resourceURI is the URI a topic is published under for resource-aware hosts.
func resourceURI(slug string) string {
	return "gossr://docs/" + slug
}

func slugFromURI(uri string) string {
	return strings.TrimPrefix(uri, "gossr://docs/")
}

// docTitle renders a human label for a topic resource.
func docTitle(t Topic) string {
	if dir := path.Dir(t.Slug); dir != "." {
		return dir + ": " + t.Title
	}
	return t.Title
}
