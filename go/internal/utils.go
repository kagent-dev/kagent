package internal

// MakePtr is a helper function to create a pointer to a value.
func MakePtr[T any](v T) *T {
	return &v
}
