package generator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

func (g *Generator) runProject(ctx context.Context) {
	args := append([]string{"run"}, reSpaces.Split(g.goRunArgs, -1)...)
	g.projectCmd = exec.CommandContext(ctx, "go", args...)
	g.projectCmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP, // Windows-specific: Create a new process group
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
		g.projectCmdDone <- struct{}{}
	}()
}

func (g *Generator) stopProject() {
	if g.projectCmd == nil || g.projectCmd.ProcessState != nil && g.projectCmd.ProcessState.Exited() {
		return
	}

	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()

	// Use GenerateConsoleCtrlEvent to send CTRL_BREAK_EVENT to the process group
	err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(g.projectCmd.Process.Pid))
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%s: %v\n", g.projectCmd, err)
		return
	}

	for {
		select {
		case <-g.projectCmdDone:
			g.projectCmd = nil
			return
		case <-timer.C:
			// Use taskkill to force kill the process tree
			cmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", g.projectCmd.Process.Pid))
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "%s: %v\n", g.projectCmd, err)
			}
		}
	}
}
