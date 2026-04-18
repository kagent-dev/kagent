package utils

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// CompressionAnnotation specifies the compression algorithm used for ConfigMap
	// values. Supported values: "gzip", "zstd". When set, all values in the
	// ConfigMap are expected to be base64-encoded compressed data and will be
	// transparently decompressed when read via GetConfigMapData.
	CompressionAnnotation = "kagent.dev/compression"
)

// GetConfigMapData fetches all data from a ConfigMap. If the ConfigMap carries
// the kagent.dev/compression annotation, values are transparently decompressed.
// Compressed values must be base64-encoded in the ConfigMap's Data field (not BinaryData).
func GetConfigMapData(ctx context.Context, c client.Client, ref client.ObjectKey) (map[string]string, error) {
	configMap := &corev1.ConfigMap{}
	if err := c.Get(ctx, ref, configMap); err != nil {
		return nil, fmt.Errorf("failed to find ConfigMap %s: %v", ref.String(), err)
	}

	algo := configMap.Annotations[CompressionAnnotation]
	if algo == "" {
		return configMap.Data, nil
	}

	decompressed := make(map[string]string, len(configMap.Data))
	for key, value := range configMap.Data {
		plain, err := decompress(value, algo)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress key %q in ConfigMap %s (algorithm=%s): %w", key, ref.String(), algo, err)
		}
		decompressed[key] = plain
	}
	return decompressed, nil
}

// decompress decodes base64 data and decompresses it with the given algorithm.
func decompress(encoded string, algo string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	switch algo {
	case "gzip":
		r, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return "", fmt.Errorf("gzip reader: %w", err)
		}
		defer r.Close()
		out, err := io.ReadAll(r)
		if err != nil {
			return "", fmt.Errorf("gzip read: %w", err)
		}
		return string(out), nil

	case "zstd":
		r, err := zstd.NewReader(bytes.NewReader(raw))
		if err != nil {
			return "", fmt.Errorf("zstd reader: %w", err)
		}
		defer r.Close()
		out, err := io.ReadAll(r)
		if err != nil {
			return "", fmt.Errorf("zstd read: %w", err)
		}
		return string(out), nil

	default:
		return "", fmt.Errorf("unsupported compression algorithm %q (supported: gzip, zstd)", algo)
	}
}

// GetConfigMapValue fetches a value from a ConfigMap
func GetConfigMapValue(ctx context.Context, c client.Client, ref client.ObjectKey, key string) (string, error) {
	configMap := &corev1.ConfigMap{}
	err := c.Get(ctx, ref, configMap)
	if err != nil {
		return "", fmt.Errorf("failed to find ConfigMap for %s: %v", ref.String(), err)
	}

	value, exists := configMap.Data[key]
	if !exists {
		return "", fmt.Errorf("key %s not found in ConfigMap %s", key, ref)
	}
	return value, nil
}
