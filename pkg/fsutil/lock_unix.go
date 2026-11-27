//go:build unix

package fsutil

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/experimentalist/envset/pkg/logger"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// LockTimeout is how long Lock waits for another envset to finish.
var LockTimeout = 20 * time.Second

// Lock takes an exclusive lock for a file envset is about to read and rewrite,
// so two runs cannot overwrite each other's change. Hold it from the read until
// the write is done, and call unlock afterwards. The lock lives in the config
// directory (never next to the file) and is keyed by the resolved path, so a
// symlink and its target share it. It is released if the process dies. It
// coordinates envset processes only; editors do not take it.
func Lock(path string) (unlock func(), err error) {
	defer logger.Enter()()
	real, err := canonicalPath(path)
	if err != nil {
		return nil, err
	}
	cfg, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(cfg, "locks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating lock directory %s: %w", dir, err)
	}
	name := fmt.Sprintf("%x.lock", sha256.Sum256([]byte(real)))
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening lock file: %w", err)
	}
	start := time.Now()
	deadline := start.Add(LockTimeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			logger.Debug("locked %s after %s (%s)", real, time.Since(start).Round(time.Millisecond), name)
			return func() { f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			f.Close()
			return nil, fmt.Errorf("locking %s: %w", Tilde(path), err)
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("another envset is still changing %s; try again", Tilde(path))
		}
		time.Sleep(20 * time.Millisecond)
	}
}
