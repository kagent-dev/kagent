package handlers

import (
	"fmt"

	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
)

// Helper function to update a reference string
func updateRef(refPtr *string, namespace string) error {
	if refPtr == nil {
		return fmt.Errorf("reference pointer cannot be nil")
	}

	ref, err := common.ParseRefString(*refPtr, namespace)
	if err != nil {
		return err
	}
	*refPtr = ref.String()
	return nil
}
