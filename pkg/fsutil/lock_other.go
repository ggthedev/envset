//go:build !unix

package fsutil

import "github.com/ggthedev/envset/pkg/logger"

import "time"

// LockTimeout is unused where file locking is not implemented.
var LockTimeout = 20 * time.Second

// Lock is a no-op on platforms without flock; CommitFile's re-check still applies.
func Lock(string) (unlock func(), err error) {
	defer logger.Enter()()
	return func() {}, nil
}
