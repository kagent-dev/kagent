package agent_test

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	translator "github.com/kagent-dev/kagent/go/internal/controller/translator/agent"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestInput represents the structure of input test files
type TestInput struct {
	Objects      []map[string]any `yaml:"objects"`
	Operation    string           `yaml:"operation"`    // "translateAgent", "translateTeam", "translateToolServer"
	TargetObject string           `yaml:"targetObject"` // name of the object to translate
	Namespace    string           `yaml:"namespace"`
	ProxyURL     string           `yaml:"proxyURL,omitempty"` // Optional proxy URL for internally-built k8s URLs
}

// TestGoldenAdkTranslator runs golden tests for the ADK API translator
func TestGoldenAdkTranslator(t *testing.T) {
	// Skip if running in CI without update flag
	updateGolden := os.Getenv("UPDATE_GOLDEN") == "true"

	testDataDir := filepath.Join("testdata")
	inputsDir := filepath.Join(testDataDir, "inputs")
	outputsDir := filepath.Join(testDataDir, "outputs")

	// Ensure directories exist
	require.DirExists(t, inputsDir, "inputs directory should exist")
	require.DirExists(t, outputsDir, "outputs directory should exist")

	// Read all input files
	inputFiles, err := filepath.Glob(filepath.Join(inputsDir, "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, inputFiles, "should have input test files")

	for _, inputFile := range inputFiles {
		// Extract test name from filename
		fileName := filepath.Base(inputFile)
		testName := strings.TrimSuffix(fileName, ".yaml")

		t.Run(testName, func(t *testing.T) {
			runGoldenTest(t, inputFile, outputsDir, testName, updateGolden)
		})
	}
}

func runGoldenTest(t *testing.T, inputFile, outputsDir, testName string, updateGolden bool) {
	ctx := context.Background()

	// Read and parse input file
	inputData, err := os.ReadFile(inputFile)
	require.NoError(t, err)

	var testInput TestInput
	err = yaml.Unmarshal(inputData, &testInput)
	require.NoError(t, err)

	// Set up fake Kubernetes client
	scheme := schemev1.Scheme
	err = v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)
	err = v1alpha1.AddToScheme(scheme)
	require.NoError(t, err)

	// Convert map objects to unstructured and then to typed objects
	clientBuilder := fake.NewClientBuilder().WithScheme(scheme)

	// Track namespaces we've seen to add them to the fake client
	namespacesSeen := make(map[string]bool)

	for _, objMap := range testInput.Objects {
		// Convert map to unstructured
		unstrObj := &unstructured.Unstructured{Object: objMap}

		// Track namespace from object metadata
		if metadata, ok := objMap["metadata"].(map[string]any); ok {
			if ns, ok := metadata["namespace"].(string); ok && ns != "" {
				namespacesSeen[ns] = true
			}
		}

		// Extract namespace from URLs in RemoteMCPServer specs
		// This is needed because isInternalK8sURL checks if the namespace exists
		if kind, ok := objMap["kind"].(string); ok && kind == "RemoteMCPServer" {
			if spec, ok := objMap["spec"].(map[string]any); ok {
				if urlStr, ok := spec["url"].(string); ok {
					if ns := extractNamespaceFromURL(urlStr); ns != "" {
						namespacesSeen[ns] = true
					}
				}
			}
		}

		// Convert to typed object
		typedObj, err := convertUnstructuredToTyped(unstrObj, scheme)
		require.NoError(t, err)
		clientBuilder = clientBuilder.WithObjects(typedObj)
	}

	// Add namespaces to fake client so namespace existence checks work
	for nsName := range namespacesSeen {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: nsName,
			},
		}
		clientBuilder = clientBuilder.WithObjects(ns)
	}

	kubeClient := clientBuilder.Build()

	// Create translator with a default model config that points to the first ModelConfig in the objects
	defaultModel := types.NamespacedName{
		Namespace: testInput.Namespace,
		Name:      "default-model",
	}

	// Try to find the first ModelConfig in the objects to use as default
	for _, objMap := range testInput.Objects {
		if kind, ok := objMap["kind"].(string); ok && kind == "ModelConfig" {
			if metadata, ok := objMap["metadata"].(map[string]any); ok {
				if name, ok := metadata["name"].(string); ok {
					defaultModel.Name = name
					break
				}
			}
		}
	}

	// Execute the specified operation
	var result any
	switch testInput.Operation {
	case "translateAgent":
		agent := &v1alpha2.Agent{}
		err := kubeClient.Get(ctx, types.NamespacedName{
			Name:      testInput.TargetObject,
			Namespace: testInput.Namespace,
		}, agent)
		require.NoError(t, err)

		// Use proxy URL from test input if provided
		proxyURL := testInput.ProxyURL
		result, err = translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, proxyURL).TranslateAgent(ctx, agent)
		require.NoError(t, err)

	default:
		t.Fatalf("unknown operation: %s", testInput.Operation)
	}

	// Serialize result to JSON for comparison
	resultJSON, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)

	// Normalize the result for deterministic comparison
	normalizedResult := normalizeJSON(t, resultJSON)

	goldenFile := filepath.Join(outputsDir, testName+".json")

	if updateGolden {
		// Update golden file
		err = os.WriteFile(goldenFile, normalizedResult, 0644)
		require.NoError(t, err)
		t.Logf("Updated golden file: %s", goldenFile)
		return
	}

	// Compare with golden file
	expectedData, err := os.ReadFile(goldenFile)
	if os.IsNotExist(err) {
		t.Fatalf("Golden file does not exist: %s. Run with UPDATE_GOLDEN=true to create it.", goldenFile)
	}
	require.NoError(t, err)

	normalizedExpected := normalizeJSON(t, expectedData)

	assert.JSONEq(t, string(normalizedExpected), string(normalizedResult),
		"Result should match golden file. Run with UPDATE_GOLDEN=true to update.")
}

func convertUnstructuredToTyped(unstrObj *unstructured.Unstructured, scheme *runtime.Scheme) (client.Object, error) {
	gvk := unstrObj.GroupVersionKind()
	obj, err := scheme.New(gvk)
	if err != nil {
		return nil, err
	}

	err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstrObj.Object, obj)
	if err != nil {
		return nil, err
	}

	clientObj, ok := obj.(client.Object)
	if !ok {
		return nil, err
	}

	return clientObj, nil
}

func normalizeJSON(t *testing.T, jsonData []byte) []byte {
	var obj any
	err := json.Unmarshal(jsonData, &obj)
	require.NoError(t, err)

	// Remove non-deterministic fields
	normalized := removeNonDeterministicFields(obj)

	result, err := json.MarshalIndent(normalized, "", "  ")
	require.NoError(t, err)

	return result
}

func removeNonDeterministicFields(obj any) any {
	switch v := obj.(type) {
	case map[string]any:
		result := make(map[string]any)
		for key, value := range v {
			// Remove fields that are non-deterministic or generated
			switch key {
			case "id", "created_at", "updated_at", "uuid":
				// Skip these fields
				continue
			default:
				result[key] = removeNonDeterministicFields(value)
			}
		}
		return result
	case []any:
		var result []any
		for _, item := range v {
			result = append(result, removeNonDeterministicFields(item))
		}
		return result
	default:
		return v
	}
}

// extractNamespaceFromURL extracts the namespace from a Kubernetes service URL.
// For example, "http://service.namespace:port/path" returns "namespace".
// Returns empty string if the URL is not a valid Kubernetes service URL.
func extractNamespaceFromURL(urlStr string) string {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}

	// Split hostname by dots: service.namespace or service.namespace.svc.cluster.local
	hostname := parsed.Hostname()
	parts := strings.Split(hostname, ".")

	// Valid patterns:
	// - service.namespace (2 parts)
	// - service.namespace.svc (3 parts)
	// - service.namespace.svc.cluster.local (5 parts)
	if len(parts) >= 2 {
		return parts[1] // namespace is always the second part
	}

	return ""
}
