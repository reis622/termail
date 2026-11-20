package single

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSecondInstanceAndRelease(t *testing.T) {
	dir := t.TempDir()
	a, busy, err := Acquire(dir)
	if err != nil || busy || a == nil {
		t.Fatalf("first: %v %v", busy, err)
	}
	if _, busy, err := Acquire(dir); err != nil || !busy {
		t.Fatalf("second instance not detected: busy=%v err=%v", busy, err)
	}
	a.Release()
	b, busy, err := Acquire(dir)
	if err != nil || busy {
		t.Fatalf("after release: %v %v", busy, err)
	}
	b.Release()
}

func TestStaleLockFileIgnored(t *testing.T) {
	dir := t.TempDir()
	// a lock file left by a crashed run (with old content) is not a lock
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, busy, err := Acquire(dir)
	if err != nil || busy {
		t.Fatalf("stale file warned: busy=%v err=%v", busy, err)
	}
	l.Release()
}

// TestOtherProcess: a lock held by another process is seen; when that
// process is killed (crash), the OS frees it.
func TestOtherProcess(t *testing.T) {
	if os.Getenv("SINGLE_HOLD") != "" {
		if _, busy, err := Acquire(os.Getenv("SINGLE_HOLD")); err != nil || busy {
			os.Exit(2)
		}
		os.Stdout.WriteString("held\n")
		select {}
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestOtherProcess")
	cmd.Env = append(os.Environ(), "SINGLE_HOLD="+dir)
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := out.Read(buf); err != nil || string(buf) != "held\n" {
		t.Fatalf("child: %q %v", buf, err)
	}
	if _, busy, _ := Acquire(dir); !busy {
		t.Fatal("lock of a running process not seen")
	}
	cmd.Process.Kill()
	cmd.Wait()
	l, busy, err := Acquire(dir)
	if err != nil || busy {
		t.Fatalf("lock not freed after crash: %v %v", busy, err)
	}
	l.Release()
}
