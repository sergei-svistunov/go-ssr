package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/sergei-svistunov/go-ssr/internal/boilerplate"
	"github.com/sergei-svistunov/go-ssr/internal/config"
	"github.com/sergei-svistunov/go-ssr/internal/generator"
)

var (
	fInit        = flag.Bool("init", false, "Initialize GoSSR config")
	fPackageName = flag.String("pkg-name", "gossr/app", "Using with -init flag")
	fWatch       = flag.Bool("watch", false, "Watch project files for changes and rebuild the project")
)

func main() {
	flag.Parse()

	if *fInit {
		if err := boilerplate.Init(*fPackageName, "internal/web"); err != nil {
			fatal(err)
		}
		fmt.Println("GoSSR project was initialized")
	}

	cfg, err := config.Read()
	if err != nil {
		fatal(err)
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
		cmd := exec.Command("go", "mod", "tidy")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
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
	_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}
