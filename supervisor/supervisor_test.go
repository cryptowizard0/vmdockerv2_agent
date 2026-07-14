// supervisor/supervisor_test.go
package supervisor

import (
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestStartCapturesOutput(t *testing.T) {
	dir := t.TempDir()
	hook := filepath.Join(dir, "start.sh")
	logPath := filepath.Join(dir, "start.log")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho hello-stdout\necho hello-stderr 1>&2\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	s := New(nil, hook, logPath)
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	var data []byte
	for time.Now().Before(deadline) {
		data, _ = os.ReadFile(logPath)
		if strings.Contains(string(data), "hello-stdout") && strings.Contains(string(data), "hello-stderr") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("log did not capture output, got %q", string(data))
}

func TestStartMissingHookIsNoOp(t *testing.T) {
	s := New(nil, filepath.Join(t.TempDir(), "nope.sh"), filepath.Join(t.TempDir(), "x.log"))
	if err := s.Start(); err != nil {
		t.Fatalf("missing hook should be a no-op, got %v", err)
	}
	if s.PGID() != 0 {
		t.Fatalf("no process should have been started, PGID=%d", s.PGID())
	}
}

func TestStartRunsProvidedCommandOverHook(t *testing.T) {
	dir := t.TempDir()
	hook := filepath.Join(dir, "start.sh")
	logPath := filepath.Join(dir, "start.log")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho from-hook\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// A provided command must run instead of the hook.
	s := New([]string{"sh", "-c", "echo from-command"}, hook, logPath)
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	var data []byte
	for time.Now().Before(deadline) {
		data, _ = os.ReadFile(logPath)
		if strings.Contains(string(data), "from-command") {
			if strings.Contains(string(data), "from-hook") {
				t.Fatalf("hook ran despite a provided command, log=%q", string(data))
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("provided command did not run, log=%q", string(data))
}

func TestReapLoopInvokesReapOnSignal(t *testing.T) {
	s := New(nil, "", "") // no process needed for this wiring test
	var calls int32
	s.reap = func() { atomic.AddInt32(&calls, 1) }

	ch := make(chan os.Signal, 1)
	go s.ReapLoop(ch)

	ch <- syscall.SIGCHLD
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&calls) >= 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("reap was not invoked on SIGCHLD, calls=%d", atomic.LoadInt32(&calls))
}

func TestForwardSignalsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	hook := filepath.Join(dir, "start.sh")
	marker := filepath.Join(dir, "term.marker")
	// Trap TERM: create the marker and exit; otherwise wait.
	script := "#!/bin/sh\ntrap 'touch " + marker + "; exit 0' TERM\n(while true; do sleep 0.1; done)\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	s := New(nil, hook, filepath.Join(dir, "start.log"))
	// Reap so the child does not linger as a zombie after it exits.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGCHLD)
	defer signal.Stop(sig)
	go s.ReapLoop(sig)

	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(200 * time.Millisecond) // let the trap install

	if err := s.Forward(syscall.SIGTERM); err != nil {
		t.Fatalf("forward: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("TERM was not delivered to the process group")
}
