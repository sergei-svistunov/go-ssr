package generator_test

import (
	"testing"

	"github.com/sergei-svistunov/go-ssr/internal/config"
	"github.com/sergei-svistunov/go-ssr/internal/generator"
)

func TestGenerator_Generate(t *testing.T) {
	g := generator.New(&config.Config{
		Dir:              "../../example",
		WebDir:           "../../example/internal/web",
		WebPackage:       "github.com/sergei-svistunov/go-ssr/example/internal/web",
		GoRunArgs:        ".",
		Env:              nil,
		GenDataProviders: true,
	})

	if err := g.Webpack(); err != nil {
		t.Fatal(err)
	}

	if err := g.Analyze(); err != nil {
		t.Fatal(err)
	}

	if err := g.Generate(); err != nil {
		t.Fatal(err)
	}
}
