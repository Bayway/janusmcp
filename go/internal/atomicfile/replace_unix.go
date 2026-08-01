//go:build !windows

package atomicfile

import "os"

// Replace atomically moves source over destination on Unix-like systems.
func Replace(source, destination string) error {
	return os.Rename(source, destination)
}
