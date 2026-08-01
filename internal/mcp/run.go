package mcp

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Running the application is how a caller finds out whether generated code
// actually compiles and serves — the generator writes and formats Go, it never
// builds it. An agent works in turns, so the application runs in the
// background and every tool call returns a snapshot of it rather than a stream.

const (
	// maxLogBytes caps what is kept per application. The tail is where a
	// failure is, and a result has to stay small enough to be worth reading.
	maxLogBytes = 64 << 10

	// logExcerptBytes is how much of the log a run or stop summary shows.
	logExcerptBytes = 4000

	defaultWait = 20 * time.Second
	maxWait     = 120 * time.Second

	// stopGrace is how long a terminated application has to exit on its own
	// before it is killed.
	stopGrace = 3 * time.Second
)

// appLog collects an application's output. It keeps only the tail, and tracks
// how much of it a caller has already been shown so gossr_logs can return what
// is new instead of repeating the whole thing.
type appLog struct {
	mu      sync.Mutex
	buf     []byte
	total   int64 // bytes ever written
	dropped int64 // bytes discarded from the front of buf
	cursor  int64 // offset in total already returned to a caller
}

func (l *appLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.total += int64(len(p))
	l.buf = append(l.buf, p...)
	if over := len(l.buf) - maxLogBytes; over > 0 {
		l.buf = append(l.buf[:0], l.buf[over:]...)
		l.dropped += int64(over)
	}
	return len(p), nil
}

// since returns the output written since the last call, along with the number
// of bytes that scrolled out of the buffer before anyone read them.
func (l *appLog) since() (string, int64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	from, missed := l.cursor, int64(0)
	if from < l.dropped {
		missed = l.dropped - from
		from = l.dropped
	}
	l.cursor = l.total

	return string(l.buf[from-l.dropped:]), missed
}

// all returns everything still buffered without moving the read cursor.
func (l *appLog) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return string(l.buf)
}

// excerpt returns the tail of the log, for a summary that should not be as long
// as the log itself.
func (l *appLog) excerpt() string {
	s := l.all()
	if len(s) <= logExcerptBytes {
		return strings.TrimSpace(s)
	}
	return "…(earlier output omitted)…\n" + strings.TrimSpace(s[len(s)-logExcerptBytes:])
}

// app is one application started through gossr_run.
type app struct {
	dir     string
	cmd     *exec.Cmd
	log     *appLog
	started time.Time
	done    chan struct{}

	mu       sync.Mutex
	url      string
	exited   bool
	exitErr  error
	exitCode int
}

func (a *app) running() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.exited
}

func (a *app) state() (running bool, code int, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.exited, a.exitCode, a.exitErr
}

func (a *app) serviceURL() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.url
}

// apps holds one application per project directory. The server outlives any
// single tool call, so the process has to be findable by the calls that come
// after the one that started it.
var (
	appsMu sync.Mutex
	apps   = map[string]*app{}
)

func lookupApp(dir string) *app {
	appsMu.Lock()
	defer appsMu.Unlock()
	return apps[dir]
}

// startApp runs the project's application in the background. It refuses to
// start a second one for the same project: two servers on one port is a
// confusing failure, and the caller almost always meant to restart.
func startApp(p *project) (*app, error) {
	appsMu.Lock()
	defer appsMu.Unlock()

	if existing := apps[p.cfg.Dir]; existing != nil && existing.running() {
		return nil, fmt.Errorf("the application is already running (pid %d); call gossr_stop first, or gossr_logs to see its output",
			existing.cmd.Process.Pid)
	}

	args := append([]string{"run"}, strings.Fields(p.cfg.GoRunArgs)...)
	cmd := exec.Command("go", args...)
	cmd.Dir = p.cfg.Dir
	// The application must never be given the stream the protocol runs on.
	cmd.Stdin = nil
	cmd.Stdout = p.out
	cmd.Stderr = p.out
	cmd.Env = os.Environ()
	for k, v := range p.cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	setProcessGroup(cmd)

	a := &app{
		dir:     p.cfg.Dir,
		cmd:     cmd,
		log:     &appLog{},
		started: time.Now(),
		done:    make(chan struct{}),
	}
	cmd.Stdout = a.log
	cmd.Stderr = a.log

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting the application: %w", err)
	}

	go func() {
		err := cmd.Wait()

		a.mu.Lock()
		a.exited = true
		a.exitErr = err
		if cmd.ProcessState != nil {
			a.exitCode = cmd.ProcessState.ExitCode()
		}
		a.mu.Unlock()

		close(a.done)
	}()

	apps[p.cfg.Dir] = a
	return a, nil
}

// reURL matches the address an application prints when it starts listening.
var reURL = regexp.MustCompile(`https?://[^\s"'<>)]+`)

// waitReady blocks until the application is answering on the address it
// printed, until it exits, or until wait elapses. An application that never
// prints a URL is not a failure — it is just one this cannot confirm.
func waitReady(a *app, wait time.Duration) {
	deadline := time.Now().Add(wait)

	for {
		if u := probeURL(a.log.all()); u != "" {
			a.mu.Lock()
			a.url = u
			a.mu.Unlock()
			return
		}

		select {
		case <-a.done:
			return
		case <-time.After(100 * time.Millisecond):
		}

		if time.Now().After(deadline) {
			return
		}
	}
}

// probeURL returns the first address in the output that accepts a connection.
func probeURL(out string) string {
	for _, raw := range reURL.FindAllString(out, 4) {
		u, err := url.Parse(strings.TrimRight(raw, ".,;"))
		if err != nil || u.Host == "" {
			continue
		}

		host := u.Hostname()
		if host == "" || host == "0.0.0.0" || host == "[::]" {
			host = "localhost"
		}
		port := u.Port()
		if port == "" {
			port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
		}

		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 300*time.Millisecond)
		if err != nil {
			continue
		}
		_ = conn.Close()

		return u.String()
	}
	return ""
}

// stopApp terminates the application and waits for it to go away, escalating to
// a kill if it does not. It reports whether there was anything to stop.
func stopApp(dir string) (*app, bool) {
	appsMu.Lock()
	a := apps[dir]
	delete(apps, dir)
	appsMu.Unlock()

	if a == nil {
		return nil, false
	}
	if !a.running() {
		return a, false
	}

	if err := terminateGroup(a.cmd); err != nil {
		// The group is already gone, or the platform refused the signal; either
		// way the wait below settles it.
		_ = killGroup(a.cmd)
	}

	select {
	case <-a.done:
	case <-time.After(stopGrace):
		_ = killGroup(a.cmd)
		<-a.done
	}

	return a, true
}

// stopAllApps stops everything this server started. A child that outlives the
// server would hold its port and could not be reached by any later call.
func stopAllApps() {
	appsMu.Lock()
	dirs := make([]string, 0, len(apps))
	for dir := range apps {
		dirs = append(dirs, dir)
	}
	appsMu.Unlock()

	for _, dir := range dirs {
		stopApp(dir)
	}
}

// describeExit renders how an application ended, for a caller that asked about
// one that is no longer running.
func describeExit(a *app) string {
	_, code, err := a.state()
	switch {
	case err == nil:
		return "the application exited normally"
	case code >= 0:
		return fmt.Sprintf("the application exited with status %d", code)
	default:
		return fmt.Sprintf("the application ended: %v", err)
	}
}
