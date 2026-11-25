package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ensureAnthropicSandboxEncryptionSecret(ctx context.Context, reader client.Reader, writer client.Writer, key client.ObjectKey) error {
	if err := reader.Get(ctx, key, &corev1.Secret{}); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("read Anthropic sandbox encryption Secret %s: %w", key, err)
	}

	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return fmt.Errorf("generate Anthropic sandbox encryption key: %w", err)
	}
	// No owner reference: PostgreSQL can retain encrypted tokens after uninstall.
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{"key": []byte(base64.StdEncoding.EncodeToString(data[:]))},
	}
	if err := writer.Create(ctx, secret); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create Anthropic sandbox encryption Secret %s: %w", key, err)
	}
	return nil
}
