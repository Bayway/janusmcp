//go:build windows

package atomicfile

import "golang.org/x/sys/windows"

// Replace atomically moves source over destination on Windows, where os.Rename
// does not replace an existing file.
func Replace(source, destination string) error {
	sourcePtr, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPtr, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		sourcePtr,
		destinationPtr,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}
