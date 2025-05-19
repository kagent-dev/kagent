package common

import (
	"context"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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

func GetRefFromString(ref string, parentNamespace string) types.NamespacedName {
	parts := strings.Split(ref, "/")
	var (
		namespace string
		name      string
	)
	if len(parts) == 2 {
		namespace = parts[0]
		name = parts[1]
	} else {
		namespace = parentNamespace
		name = ref
	}

	return types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
}

func FetchObjKube(ctx context.Context, kube client.Client, obj client.Object, objName, objNamespace string) error {
	ref := GetRefFromString(objName, objNamespace)

	err := kube.Get(ctx, ref, obj)
	if err != nil {
		return err
	}

	return nil
}

func ConvertToPythonIdentifier(name string) string {
	name = strings.ReplaceAll(name, "-", "_")
	return strings.ReplaceAll(name, "/", "__NS__") // RFC 1123 will guarantee there will be no conflicts
}

func ConvertToKubernetesIdentifier(name string) string {
	name = strings.ReplaceAll(name, "__NS__", "/")
	return strings.ReplaceAll(name, "_", "-")
}
