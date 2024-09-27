package generator_test

import (
	"testing"

	"github.com/sergei-svistunov/go-ssr/internal/generator"
)

func TestGenerator_Generate(t *testing.T) {
	g := generator.New("../../example/internal/web", "github.com/sergei-svistunov/go-ssr/example/internal/web")

	if err := g.Analyze(); err != nil {
		t.Fatal(err)
	}

	if err := g.Generate(); err != nil {
		t.Fatal(err)
	}
}
