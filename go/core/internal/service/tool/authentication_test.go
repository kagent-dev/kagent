// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type mcpTestTokenSource struct {
	token string
	err   error
}

func (s *mcpTestTokenSource) Token(context.Context) (string, error) { return s.token, s.err }

func TestMCPAuthenticationBindsAndRenewsConnectionCredentials(t *testing.T) {
	for _, principal := range []string{"human", "machine"} {
		t.Run(principal, func(t *testing.T) {
			received := make(chan http.Header, 8)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received <- r.Header.Clone()
				w.WriteHeader(http.StatusOK)
			}))
			defer upstream.Close()
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "tenant"}, Data: map[string][]byte{"key": []byte("tenant-key")}}
			kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
			service := NewRuntimeMCPClient(kube)
			source := &mcpTestTokenSource{token: principal + "-token"}
			ctx := auth.AuthSessionTo(t.Context(), auth.NewCredentialSession(auth.Principal{ID: principal}, source))
			server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Namespace: "team"}, Spec: v1alpha3.RemoteMCPServerSpec{URL: upstream.URL, Headers: []v1alpha3.Header{
				{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}},
				{Name: "X-Tenant-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "tenant"}, Key: "key"}}},
			},
			}}
			client, err := service.authenticatedMCPClient(ctx, server)
			require.NoError(t, err)
			send := func() error {
				other := auth.AuthSessionTo(t.Context(), auth.NewCredentialSession(auth.Principal{ID: "other"}, &mcpTestTokenSource{token: "other-token"}))
				req, err := http.NewRequestWithContext(other, http.MethodGet, upstream.URL, nil)
				require.NoError(t, err)
				response, err := client.Do(req)
				if response != nil {
					_, _ = io.Copy(io.Discard, response.Body)
					response.Body.Close()
				}
				return err
			}
			require.NoError(t, send())
			headers := <-received
			require.Equal(t, "Bearer "+principal+"-token", headers.Get("Authorization"))
			require.Equal(t, "tenant-key", headers.Get("X-Tenant-Key"))
			source.token = principal + "-rotated"
			secret.Data["key"] = []byte("rotated-tenant-key")
			require.NoError(t, kube.Update(t.Context(), secret))
			require.NoError(t, send())
			headers = <-received
			require.Equal(t, "Bearer "+source.token, headers.Get("Authorization"))
			require.Equal(t, "rotated-tenant-key", headers.Get("X-Tenant-Key"))
			source.err = errors.New("private authentication error")
			err = send()
			require.ErrorContains(t, err, "credential is unavailable or expired")
			require.NotContains(t, err.Error(), source.err.Error())
			require.Empty(t, received)
			noCaller, err := service.authenticatedMCPClient(t.Context(), server)
			require.NoError(t, err)
			_, err = noCaller.Get(upstream.URL)
			require.ErrorContains(t, err, "invocation credential is unavailable")
			other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("credential request crossed origins") }))
			defer other.Close()
			_, err = client.Get(other.URL)
			require.ErrorContains(t, err, "changed destination")
		})
	}
}
