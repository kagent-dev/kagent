// Package fs provides filesystem utilities.
package fs

import "os"

// FileExists checks if a file exists at the given path.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
