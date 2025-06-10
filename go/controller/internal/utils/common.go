package common

import "os"

func GetResourceNamespace() string {
	if val := os.Getenv("KAGENT_NAMESPACE"); val != "" {
		return val
	}
	return "kagent"
}

func GetGlobalUserID() string {
	if val := os.Getenv("KAGENT_GLOBAL_USER_ID"); val != "" {
		return val
	}
	return "admin@kagent.dev"
}

// MakePtr is a helper function to create a pointer to a value.
func MakePtr[T any](v T) *T {
	return &v
}
