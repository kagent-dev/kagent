package utils

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

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

	// maxDecompressedSize is the upper bound on decompressed output (10 MB).
	// This prevents a small compressed payload from expanding into an
	// arbitrarily large allocation that could OOM the controller.
	maxDecompressedSize = 10 << 20 // 10 MiB
)

// GetConfigMapData fetches all data from a ConfigMap. If the ConfigMap carries
// the kagent.dev/compression annotation, values are transparently decompressed.
// Compressed values must be base64-encoded in the ConfigMap's Data field (not BinaryData).
func GetConfigMapData(ctx context.Context, c client.Client, ref client.ObjectKey) (map[string]string, error) {
	configMap := &corev1.ConfigMap{}
	if err := c.Get(ctx, ref, configMap); err != nil {
		return nil, fmt.Errorf("failed to find ConfigMap %s: %w", ref.String(), err)
	}

	algo := strings.ToLower(strings.TrimSpace(configMap.Annotations[CompressionAnnotation]))
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
// The encoded payload is whitespace-tolerant (newlines and spaces are stripped
// before decoding) and decompressed output is capped at maxDecompressedSize.
func decompress(encoded string, algo string) (string, error) {
	// Strip whitespace/newlines that commonly appear in pasted base64
	cleaned := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, encoded)

	raw, err := base64.StdEncoding.DecodeString(cleaned)
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
		out, err := io.ReadAll(io.LimitReader(r, maxDecompressedSize+1))
		if err != nil {
			return "", fmt.Errorf("gzip read: %w", err)
		}
		if len(out) > maxDecompressedSize {
			return "", fmt.Errorf("decompressed output exceeds %d bytes limit", maxDecompressedSize)
		}
		return string(out), nil

	case "zstd":
		r, err := zstd.NewReader(bytes.NewReader(raw))
		if err != nil {
			return "", fmt.Errorf("zstd reader: %w", err)
		}
		defer r.Close()
		out, err := io.ReadAll(io.LimitReader(r, maxDecompressedSize+1))
		if err != nil {
			return "", fmt.Errorf("zstd read: %w", err)
		}
		if len(out) > maxDecompressedSize {
			return "", fmt.Errorf("decompressed output exceeds %d bytes limit", maxDecompressedSize)
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
		return "", fmt.Errorf("failed to find ConfigMap for %s: %w", ref.String(), err)
	}

	value, exists := configMap.Data[key]
	if !exists {
		return "", fmt.Errorf("key %s not found in ConfigMap %s", key, ref)
	}
	return value, nil
}
