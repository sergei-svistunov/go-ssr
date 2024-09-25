package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sergei-svistunov/go-ssr/internal/generator"
)

var (
	pagesDir    = flag.String("dir", ".", "Directory with folder pages")
	packageName = flag.String("package", "handler", "Package name")
)

func main() {
	flag.Parse()

	g := generator.New(*pagesDir, *packageName)
	if err := g.Analyze(); err != nil {
		fatal(err)
	}
	if err := g.Generate(); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}
