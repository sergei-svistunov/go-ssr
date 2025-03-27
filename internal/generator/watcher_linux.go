package generator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"syscall"
	"time"
)

var reSpaces = regexp.MustCompile(`\s+`)

func (g *Generator) runProject(ctx context.Context) {
	args := append([]string{"run"}, reSpaces.Split(g.goRunArgs, -1)...)
	g.projectCmd = exec.CommandContext(ctx, "go", args...)
	g.projectCmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	g.projectCmd.Stdin = os.Stdin
	g.projectCmd.Stdout = os.Stdout
	g.projectCmd.Stderr = os.Stderr
	g.projectCmd.Env = os.Environ()
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
