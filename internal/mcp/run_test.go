package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
)

// gossr_logs returns what is new, so an agent polling it does not re-read the
// same output every time — and says so when output was lost.
func TestAppLog_ReturnsOnlyWhatIsNew(t *testing.T) {
	l := &appLog{}

	fmt.Fprint(l, "starting\n")
	if got, missed := l.since(); got != "starting\n" || missed != 0 {
		t.Fatalf("since() = %q, %d; want the first line and nothing missed", got, missed)
	}
	if got, missed := l.since(); got != "" || missed != 0 {
		t.Errorf("a second call returned %q, %d; want nothing", got, missed)
	}

	fmt.Fprint(l, "listening\n")
	if got, _ := l.since(); got != "listening\n" {
		t.Errorf("since() = %q, want only the new line", got)
	}
}

func TestAppLog_KeepsTheTailAndReportsWhatWasLost(t *testing.T) {
	l := &appLog{}

	fmt.Fprint(l, strings.Repeat("x", maxLogBytes))
	fmt.Fprint(l, "tail")

	if len(l.all()) > maxLogBytes {
		t.Errorf("buffer grew to %d bytes, past the %d cap", len(l.all()), maxLogBytes)
	}
	if !strings.HasSuffix(l.all(), "tail") {
		t.Error("the newest output was dropped instead of the oldest")
	}

	got, missed := l.since()
	if missed != 4 {
		t.Errorf("missed = %d, want the 4 bytes that scrolled out", missed)
	}
	if !strings.HasSuffix(got, "tail") {
		t.Errorf("since() lost the tail: %q", got[max(0, len(got)-20):])
	}
}

// The address is confirmed by connecting to it: an application that prints a URL
// and then fails to bind must not be reported as ready.
func TestProbeURL_OnlyReportsAnAddressThatAnswers(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	go http.Serve(ln, http.NotFoundHandler())

	live := fmt.Sprintf("http://localhost:%d/", ln.Addr().(*net.TCPAddr).Port)

	if got := probeURL("Server listens on " + live + "\n"); got != live {
		t.Errorf("probeURL = %q, want %q", got, live)
	}
	if got := probeURL("see http://localhost:1/ for details"); got != "" {
		t.Errorf("probeURL = %q for an address nothing answers on, want empty", got)
	}
	if got := probeURL("no address here"); got != "" {
		t.Errorf("probeURL = %q, want empty", got)
	}
}

func TestLogsTool_SaysSoWhenNothingWasStarted(t *testing.T) {
	dir := fixtureProject(t)

	_, _, err := handleLogs(context.Background(), nil, logsInput{ProjectDir: dir})
	if err == nil {
		t.Fatal("expected an error when no application has been started")
	}
	if !strings.Contains(err.Error(), "gossr_run") {
		t.Errorf("the error should name the tool that starts one: %v", err)
	}
}

// Stopping something that is not running is the state the caller asked for.
func TestStopTool_IsANoOpWhenNothingIsRunning(t *testing.T) {
	dir := fixtureProject(t)

	res, out, err := handleStop(context.Background(), nil, projectInput{ProjectDir: dir})
	if err != nil {
		t.Fatalf("handleStop: %v", err)
	}
	if res.IsError || !out.OK {
		t.Error("stopping nothing must not be reported as a failure")
	}
	if out.Stopped {
		t.Error("nothing was running, so nothing was stopped")
	}
}
