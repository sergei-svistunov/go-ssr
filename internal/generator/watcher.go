package generator

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

func (g *Generator) Watch(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	fillWatcher := func() {
		if err := filepath.Walk(g.dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.IsDir() && !g.ignoreFile(path) {
				err = watcher.Add(path)
				if err != nil {
					return err
				}
				//log.Printf("Watching directory: %s\n", path)
			}
			return nil
		}); err != nil {
			log.Printf("Cannot watch: %s", err)
		}
	}

	timer := time.NewTimer(0)
	affectWebpack := false
	affectSSR := false
	affectGo := false

	g.runProject(ctx)

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				//log.Println("event:", event)
				if !g.ignoreFile(event.Name) {
					fileExt := filepath.Ext(event.Name)
					if needWebpack(fileExt) {
						affectWebpack = true
					}
					if fileExt == ".html" && filepathHasPrefix(event.Name, g.webDir) {
						affectSSR = true
					}
					if fileExt == ".go" {
						affectGo = true
					}
					if event.Has(fsnotify.Create) && isDir(event.Name) {
						err = watcher.Add(event.Name)
						if err != nil {
							log.Printf("Cannot add dir %s to watch: %s", event.Name, err)
						}
					}
				} else {
					log.Println("Ignore")
				}
				timer.Reset(300 * time.Millisecond)
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("error:", err)

			case <-timer.C:
				if !(affectWebpack || affectSSR || affectGo) {
					continue
				}
				g.stopProject()
				for _, n := range watcher.WatchList() {
					if err := watcher.Remove(n); err != nil {
						log.Printf("Cannot remove %s from watcher: %s", n, err)
					}
				}

				if affectWebpack {
					log.Println("---------REGENERATE STATIC---------")
					if err := g.Webpack(); err != nil {
						_, _ = fmt.Fprintf(os.Stderr, "%v\n", err)
						fillWatcher()
						continue
					}
				}
				if affectWebpack || affectSSR {
					log.Println("---------REGENERATE SSR---------")
					if err := g.Analyze(); err != nil {
						_, _ = fmt.Fprintf(os.Stderr, "%v\n", err)
						fillWatcher()
						continue
					}
					if err := g.Generate(); err != nil {
						_, _ = fmt.Fprintf(os.Stderr, "%v\n", err)
						fillWatcher()
						continue
					}
				}
				log.Println("---------REBUILD---------")
				g.runProject(ctx)

				fillWatcher()
				affectWebpack = false
				affectSSR = false
				affectGo = false
			}
		}
	}()

	fillWatcher()

	<-ctx.Done()

	return nil
}

func (g *Generator) Shutdown() {
	g.stopProject()
}

func (g *Generator) runProject(ctx context.Context) {
	g.projectCmd = exec.CommandContext(ctx, "go", "run", g.goRunArgs)
	g.projectCmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	g.projectCmd.Stdin = os.Stdin
	g.projectCmd.Stdout = os.Stdout
	g.projectCmd.Stderr = os.Stderr
	for k, v := range g.projectCmdEnv {
		g.projectCmd.Env = append(g.projectCmd.Env, k+"="+v)
	}

	if err := g.projectCmd.Start(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return
	}

	if g.projectCmdDone == nil {
		g.projectCmdDone = make(chan struct{})
	}

	go func() {
		if err := g.projectCmd.Wait(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "%s: %v\n", g.projectCmd, err)
		}
		//if err := g.projectCmd.Process.Release(); err != nil {
		//	_, _ = fmt.Fprintf(os.Stderr, "%s: %v\n", g.projectCmd, err)
		//}
		g.projectCmdDone <- struct{}{}
	}()
}

func (g *Generator) stopProject() {
	if g.projectCmd == nil || g.projectCmd.ProcessState != nil && g.projectCmd.ProcessState.Exited() {
		return
	}

	timer := time.NewTimer(3 * time.Second)

	if err := syscall.Kill(-g.projectCmd.Process.Pid, syscall.SIGTERM); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%s: %v\n", g.projectCmd, err)
		return
	}

	for {
		select {
		case <-g.projectCmdDone:
			g.projectCmd = nil
			return
		case <-timer.C:
			if err := syscall.Kill(-g.projectCmd.Process.Pid, syscall.SIGKILL); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "%s: %v\n", g.projectCmd, err)
			}
		}
	}
}

func (g *Generator) ignoreFile(file string) bool {
	return filepathHasPrefix(file, filepath.Join(g.webDir, g.assets.outputPath)) ||
		filepathHasPrefix(file, filepath.Join(g.webDir, "node_modules/.cache"))
}

func needWebpack(fileExt string) bool {
	for _, ext := range []string{".scss", ".ts", ".css", ".js", ".json", ".png", ".jpg", ".jpeg", ".gif", ".svg"} {
		if fileExt == ext {
			return true
		}
	}
	return false
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	if err != nil {
		log.Print(err)
		return false
	}
	return fi.IsDir()
}

func filepathHasPrefix(p, prefix string) bool {
	absPath, err := filepath.Abs(p)
	if err != nil {
		log.Printf("Cannot get absolute path for %s: %s", p, err)
		return false
	}

	absPrefix, err := filepath.Abs(prefix)
	if err != nil {
		log.Printf("Cannot get absolute path for %s: %s", prefix, err)
		return false
	}

	return absPath == absPrefix || strings.HasPrefix(absPath, absPrefix+"/")
}
