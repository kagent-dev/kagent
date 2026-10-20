package image

import "regexp"

var digestPinned = regexp.MustCompile(`^[^[:space:]@]+@sha256:[a-f0-9]{64}$`)

// IsDigestPinned reports whether reference names an image pinned by SHA-256 digest.
func IsDigestPinned(reference string) bool {
	return digestPinned.MatchString(reference)
}
