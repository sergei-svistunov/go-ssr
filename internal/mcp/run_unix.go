//go:build unix

package mcp

import (
	"os/exec"
	"syscall"
)

// `go run` compiles the application and starts the result as a child of its
// own, so signalling only the command leaves the application running. Putting
// it in a new process group gives us something to signal that covers both.

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateGroup(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}

func killGroup(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
