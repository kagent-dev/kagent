package handlers

import common "github.com/kagent-dev/kagent/go/controller/internal/utils"

// Helper function to update a reference string
func updateRef(refPtr *string, namespace string) error {
	ref, err := common.ParseRefString(*refPtr, namespace)
	if err != nil {
		return err
	}
	*refPtr = ref.String()
	return nil
}
