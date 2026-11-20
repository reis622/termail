// Package single tells whether another Termail is already running, with an
// OS lock (Windows: LockFileEx, elsewhere: flock) on a small file. The OS
// drops the lock when the process ends, even after a crash, so a left-over
// lock file never causes a warning; only a running Termail does.
package single

import (
	"os"
	"path/filepath"
)

// Lock is held for the life of the process.
type Lock struct{ f *os.File }

// FileName is the lock file inside the Termail folder.
const FileName = "termail.lock"

// Acquire takes the lock in dir. busy is true only when another running
// process holds it. On other errors (no folder, read-only disk) Termail
// just runs without the check: err is returned, busy is false.
func Acquire(dir string) (l *Lock, busy bool, err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(filepath.Join(dir, FileName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, false, err
	}
	held, err := tryLock(f)
	if err != nil || !held {
		f.Close()
		return nil, err == nil && !held, err
	}
	return &Lock{f: f}, false, nil
}

// Release frees the lock (also done by the OS at exit).
func (l *Lock) Release() {
	if l == nil || l.f == nil {
		return
	}
	unlock(l.f)
	l.f.Close()
	l.f = nil
}
