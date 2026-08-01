package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sergei-svistunov/go-ssr/internal/boilerplate"
	"github.com/sergei-svistunov/go-ssr/internal/config"
	"github.com/sergei-svistunov/go-ssr/internal/generator"
	"github.com/sergei-svistunov/go-ssr/internal/mcp"
)

var (
	fInit        = flag.Bool("init", false, "Initialize GoSSR config")
	fPackageName = flag.String("pkg-name", "gossr/app", "Using with -init flag")
	fWatch       = flag.Bool("watch", false, "Watch project files for changes and rebuild the project")
	fProd        = flag.Bool("prod", false, "Build static files for production")
	fMCP         = flag.Bool("mcp", false, "Serve the documentation and project tools over MCP on stdio")
)

func main() {
	flag.Parse()

	if *fMCP {
		// Served before any config lookup: the documentation tools are useful
		// with no project present, for instance when creating one.
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer stop()

		if err := mcp.Serve(ctx); err != nil {
			fatal(err)
		}
		return
	}

	if *fInit {
		if err := boilerplate.Init(boilerplate.Options{Dir: ".", PkgName: *fPackageName}); err != nil {
			fatal(err)
		}
		fmt.Println("GoSSR project was initialized")
	}

	cfg, err := config.Read()
	if err != nil {
		fatal(err)
	}
	if *fProd {
		cfg.Prod = true
	}

	g := generator.New(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := g.Webpack(); err != nil {
		fatal(err)
	}
	if err := g.Analyze(); err != nil {
		fatal(err)
	}
	if err := g.Generate(); err != nil {
		fatal(err)
	}

	if *fInit {
		if err := g.GoModTidy(); err != nil {
			fatal(err)
		}
	}

	if *fWatch {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
		go func() {
			<-sigChan
			g.Shutdown()
			cancel()
		}()

		if err := g.Watch(ctx); err != nil {
			cancel()
			fatal(err)
		}
		<-ctx.Done()
	}
}

func fatal(err error) {
	// Template and validation failures arrive as a set of located diagnostics;
	// print one per line so every broken route is visible at once.
	var diags generator.Diagnostics
	if errors.As(err, &diags) {
		for _, d := range diags {
			_, _ = fmt.Fprintln(os.Stderr, d.Error())
		}
		os.Exit(1)
	}

	_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}
