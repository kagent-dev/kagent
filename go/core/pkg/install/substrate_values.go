package install

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"
)

const rustFSCredentialsSecret = "kagent-rustfs-credentials"

type rustFSValues struct {
	Enabled   bool   `json:"enabled"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
}

type substrateStorageValues struct {
	RustFS rustFSValues `json:"rustfs"`
	Atelet struct {
		StorageBackend string `json:"storageBackend"`
	} `json:"atelet"`
}

type ateletStorageValues struct {
	ObjectStore struct {
		Backend           string `json:"backend,omitempty"`
		Endpoint          string `json:"endpoint,omitempty"`
		Region            string `json:"region,omitempty"`
		PathStyle         bool   `json:"pathStyle,omitempty"`
		CredentialsSecret string `json:"credentialsSecret,omitempty"`
	} `json:"objectStore"`
}

// Resolve shared overrides as one layer so Atelet-specific files can override
// even Substrate --set values. Only storage derivation uses the chart defaults;
// other defaults remain owned by each chart.
func resolveAteletValues(ctx context.Context, runner Runner, substrate, atelet Chart) (resolved Chart, storage rustFSValues, resultErr error) {
	directory, err := os.MkdirTemp("", "kagent-substrate-values-")
	if err != nil {
		return Chart{}, storage, fmt.Errorf("failed to create Substrate values workspace: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(directory); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("failed to remove Substrate values workspace: %w", err))
		}
	}()
	if err := writeValuesChart(directory); err != nil {
		return Chart{}, storage, err
	}
	args := []string{"show", "values", chartReference(substrate), "--version", substrate.Version}
	args = append(args, chartTransportArgs(substrate)...)
	defaults, err := runner.Run(ctx, Command{Program: "helm", Args: args})
	if err != nil {
		return Chart{}, storage, fmt.Errorf("failed to read Substrate chart defaults: %w", &valuesError{cause: err})
	}
	path := filepath.Join(directory, "values.yaml")
	if err := os.WriteFile(path, []byte(defaults), 0o600); err != nil {
		return Chart{}, storage, fmt.Errorf("failed to write Substrate chart defaults: %w", err)
	}
	values, err := mergeValues(ctx, runner, substrate, directory)
	if err != nil {
		return Chart{}, storage, err
	}
	data, err := json.Marshal(values)
	if err != nil {
		return Chart{}, storage, &valuesError{cause: err}
	}
	var source substrateStorageValues
	if err := json.Unmarshal(data, &source); err != nil {
		return Chart{}, storage, fmt.Errorf("failed to decode Substrate storage settings: %w", &valuesError{cause: err})
	}
	// The separate chart renamed the storage settings. These are defaults for
	// Atelet; shared objectStore values and then Atelet overrides take precedence.
	node := ateletStorageValues{}
	node.ObjectStore.Backend = source.Atelet.StorageBackend
	if source.RustFS.Enabled {
		node.ObjectStore.Endpoint = "http://rustfs." + substrateNamespace + ".svc:9000"
		node.ObjectStore.Region = "us-east-1"
		node.ObjectStore.PathStyle = true
		node.ObjectStore.CredentialsSecret = rustFSCredentialsSecret
	}
	data, err = yaml.Marshal(node)
	if err != nil {
		return Chart{}, storage, &valuesError{cause: err}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return Chart{}, storage, fmt.Errorf("failed to write Atelet storage defaults: %w", err)
	}
	shared, err := mergeValues(ctx, runner, substrate, directory)
	if err != nil {
		return Chart{}, storage, err
	}
	data, err = yaml.Marshal(shared)
	if err != nil {
		return Chart{}, storage, &valuesError{cause: err}
	}
	resolved = cloneChart(atelet)
	resolved.inlineValues = string(data)
	return resolved, source.RustFS, nil
}
