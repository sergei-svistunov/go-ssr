//go:build unix

package mcp

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/sergei-svistunov/go-ssr/internal/config"
)

// Cancelling the context an application runs under stops the program go run
// started, not only go run itself.
func TestStartApp_CancelStopsTheApplication(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module app\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

import (
	"os"
	"strconv"
	"time"
)

func main() {
	_ = os.WriteFile("pid", []byte(strconv.Itoa(os.Getpid())), 0o600)
	time.Sleep(time.Hour)
}
`)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	a, err := startApp(ctx, &project{cfg: &config.Config{Dir: dir, GoRunArgs: "."}, out: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("startApp: %v", err)
	}
	t.Cleanup(func() { stopApp(dir) })
	pid := waitPid(t, a, dir)

	cancel()
	select {
	case <-a.done:
	case <-time.After(10 * time.Second):
		t.Fatal("go run did not exit after the context was cancelled")
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the application outlived go run after the context was cancelled")
		}
	}
}

// An application that ignores the terminate signal does not hold up the end of
// go run past the stop grace, and is killed with it.
func TestStartApp_CancelDoesNotWaitForAnApplicationThatIgnoresIt(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module app\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

import (
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func main() {
	signal.Ignore(syscall.SIGTERM)
	_ = os.WriteFile("pid", []byte(strconv.Itoa(os.Getpid())), 0o600)
	time.Sleep(time.Hour)
}
`)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	a, err := startApp(ctx, &project{cfg: &config.Config{Dir: dir, GoRunArgs: "."}, out: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("startApp: %v", err)
	}
	t.Cleanup(func() { stopApp(dir) })
	pid := waitPid(t, a, dir)

	cancel()
	select {
	case <-a.done:
	case <-time.After(stopGrace + 5*time.Second):
		t.Fatal("go run did not end within the stop grace after the context was cancelled")
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the application outlived go run after the context was cancelled")
		}
	}
}

// waitPid returns the pid the application in dir writes when it starts, and
// kills it when the test ends.
func waitPid(t *testing.T, a *app, dir string) int {
	t.Helper()
	pid := 0
	for deadline := time.Now().Add(time.Minute); pid == 0 && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if b, err := os.ReadFile(filepath.Join(dir, "pid")); err == nil {
			pid, _ = strconv.Atoi(string(b))
		}
	}
	if pid == 0 {
		t.Fatalf("the application did not start: %s", a.log.all())
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	return pid
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
