package mcp

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The corpus is what an agent is told is authoritative, so wrong documentation
// is worse than missing documentation. These tests tie it to the parser and the
// generator: adding syntax or a diagnostic code without writing it up fails here.

func TestDocs_EveryTopicIsWellFormed(t *testing.T) {
	all := topics()
	if len(all) < 10 {
		t.Fatalf("only %d topics found; the corpus is not embedded", len(all))
	}

	for _, topic := range all {
		if topic.Title == topic.Slug {
			t.Errorf("topic %s has no '# ' heading", topic.Slug)
		}
		if topic.Summary == "" {
			t.Errorf("topic %s has no summary paragraph after its heading", topic.Slug)
		}
		if len(sectionNames(topic)) == 0 && !strings.HasPrefix(topic.Slug, "recipes/") {
			t.Errorf("topic %s has no '## ' sections", topic.Slug)
		}
		if _, err := findTopic(topic.Slug); err != nil {
			t.Errorf("topic %s does not resolve: %v", topic.Slug, err)
		}
	}
}

// A '#' comment inside a fenced code block is not a heading. Reading it as one
// truncates the section at the first shell or Dockerfile comment and hands the
// caller an unclosed code block with none of the commands in it.
func TestDocs_SectionsSurviveCommentsInsideCodeFences(t *testing.T) {
	body := `# Title

Summary.

## Typical use

` + "```bash" + `
# scaffold
go-ssr -init

# regenerate
go-ssr
` + "```" + `

## Next section

after
`
	topic := parseTopic("t", body)

	got, err := section(topic, "Typical use")
	if err != nil {
		t.Fatalf("section: %v", err)
	}
	for _, want := range []string{"go-ssr -init", "# regenerate", "go-ssr\n```"} {
		if !strings.Contains(got, want) {
			t.Errorf("section is truncated, %q is missing:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Next section") {
		t.Errorf("section ran past its own heading:\n%s", got)
	}

	if names := sectionNames(topic); !slices.Equal(names, []string{"Typical use", "Next section"}) {
		t.Errorf("sectionNames = %v, want the two real headings", names)
	}
}

// The same trap in the shipped corpus: every section a caller can ask for must
// come back with its code fences closed.
func TestDocs_EverySectionOfEveryTopicIsComplete(t *testing.T) {
	for _, topic := range topics() {
		for _, name := range sectionNames(topic) {
			body, err := section(topic, name)
			if err != nil {
				t.Errorf("%s section %q: %v", topic.Slug, name, err)
				continue
			}
			if strings.Count(body, "```")%2 != 0 {
				t.Errorf("%s section %q ends inside a code fence:\n%s", topic.Slug, name, body)
			}
		}
	}
}

func TestDocs_EveryParserTagAndAttributeIsDocumented(t *testing.T) {
	corpus := wholeCorpus(t)

	tags := switchCases(t, "string(tagName[4:])")
	if len(tags) < 5 {
		t.Fatalf("found %d ssr: tags in the parser; the switch this test reads has moved", len(tags))
	}
	for _, name := range tags {
		if !strings.Contains(corpus, "ssr:"+name) {
			t.Errorf("the parser accepts <ssr:%s> but no documentation topic mentions it", name)
		}
	}

	attrs := switchCases(t, "key")
	if !slices.Contains(attrs, "if") || !slices.Contains(attrs, "for") {
		t.Fatalf("found %v as ssr: attributes; the switch this test reads has moved", attrs)
	}
	for _, name := range attrs {
		if !strings.Contains(corpus, "ssr:"+name) {
			t.Errorf("the parser accepts ssr:%s but no documentation topic mentions it", name)
		}
	}

	t.Logf("documented tags %v, attributes %v", tags, attrs)
}

func TestDocs_EveryDiagnosticCodeIsDocumented(t *testing.T) {
	errorsTopic, err := findTopic("errors")
	if err != nil {
		t.Fatal(err)
	}

	codes := diagnosticCodes(t)
	if len(codes) == 0 {
		t.Fatal("no diagnostic codes found in the generator; this test no longer reads them correctly")
	}

	for _, code := range codes {
		if !strings.Contains(errorsTopic.Body, code) {
			t.Errorf("the generator reports %s but docs/errors.md does not explain it", code)
		}
	}

	t.Logf("documented codes %v", codes)
}

// Cross-references between topics are written as `slug`, and a broken one sends a
// reader to a topic that does not exist.
func TestDocs_CrossReferencesResolve(t *testing.T) {
	ref := regexp.MustCompile("`(recipes/[a-z-]+|[a-z][a-z-]{3,})`")

	for _, topic := range topics() {
		for _, m := range ref.FindAllStringSubmatch(topic.Body, -1) {
			slug := m[1]
			// Only slugs that look like topics are checked; the same syntax is
			// used for code spans everywhere else.
			if !strings.HasPrefix(slug, "recipes/") && !isTopicSlug(slug) {
				continue
			}
			if _, err := findTopic(slug); err != nil {
				t.Errorf("topic %s references `%s`, which is not a topic", topic.Slug, slug)
			}
		}
	}
}

func isTopicSlug(s string) bool {
	_, ok := topicIndex()[s]
	return ok
}

func wholeCorpus(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, topic := range topics() {
		b.WriteString(topic.Body)
		b.WriteByte('\n')
	}
	return b.String()
}

// switchCases returns the string case values of the switch statement in the
// template parser whose tag prints as want.
func switchCases(t *testing.T, want string) []string {
	t.Helper()

	path := filepath.Join("..", "generator", "route", "template", "template.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	var found []string
	ast.Inspect(file, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok || sw.Tag == nil {
			return true
		}
		var tag strings.Builder
		if err := printer.Fprint(&tag, fset, sw.Tag); err != nil {
			return true
		}
		if tag.String() != want {
			return true
		}
		for _, stmt := range sw.Body.List {
			clause, ok := stmt.(*ast.CaseClause)
			if !ok {
				continue
			}
			for _, expr := range clause.List {
				lit, ok := expr.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				s, err := strconv.Unquote(lit.Value)
				if err == nil && s != "" {
					found = append(found, s)
				}
			}
		}
		return true
	})

	sort.Strings(found)
	return slices.Compact(found)
}

// diagnosticCodes collects the codes the generator attaches to diagnostics.
func diagnosticCodes(t *testing.T) []string {
	t.Helper()

	dir := filepath.Join("..", "generator")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(`Code(?::|\s*=)\s*"([A-Z]\d+)"`)
	var codes []string

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(body), -1) {
			codes = append(codes, m[1])
		}
	}

	sort.Strings(codes)
	return slices.Compact(codes)
}
