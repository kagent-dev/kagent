package model_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/model"
	"github.com/kagent-dev/kagent/go/core/internal/service/secretmaterial"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type denyAuthorizer struct{}

func (denyAuthorizer) Check(_ context.Context, _ pkgauth.Principal, _ pkgauth.Verb, _ pkgauth.Resource) error {
	return errors.New("denied")
}

func TestServiceCRUDAndValidation(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	newService := func(authorizer pkgauth.Authorizer, objects ...ctrlclient.Object) (*model.Service, ctrlclient.Client, context.Context) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
		service := model.NewService(kubeClient, authorizer, "default")
		ctx := pkgauth.AuthSessionTo(context.Background(), &authimpl.SimpleSession{P: pkgauth.Principal{User: pkgauth.User{ID: "test-user"}}})
		return service, kubeClient, ctx
	}

	t.Run("list and get", func(t *testing.T) {
		service, _, ctx := newService(&authimpl.NoopAuthorizer{}, &v1alpha2.ModelConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "default"},
			Spec:       v1alpha2.ModelConfigSpec{Model: "gpt-4", Provider: v1alpha2.ModelProviderOpenAI},
		})

		list, err := service.List(ctx, model.ListRequest{})
		require.NoError(t, err)
		require.Len(t, list.Items, 1)

		got, err := service.Get(ctx, model.GetRequest{Ref: types.NamespacedName{Namespace: "default", Name: "cfg"}})
		require.NoError(t, err)
		assert.Equal(t, "gpt-4", got.Spec.Model)
	})

	t.Run("create defaults api key secret and writes secret", func(t *testing.T) {
		service, kubeClient, ctx := newService(&authimpl.NoopAuthorizer{})

		created, err := service.Create(ctx, model.CreateRequest{
			Ref:    "test-config",
			APIKey: "inline-secret",
			Spec: v1alpha2.ModelConfigSpec{
				Model:    "gpt-4",
				Provider: v1alpha2.ModelProviderOpenAI,
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "test-config", created.Spec.APIKeySecret)
		assert.Equal(t, "OPENAI_API_KEY", created.Spec.APIKeySecretKey)

		secret := &corev1.Secret{}
		err = kubeClient.Get(ctx, ctrlclient.ObjectKey{Namespace: "default", Name: "test-config"}, secret)
		require.NoError(t, err)
		assert.Equal(t, "inline-secret", string(secret.Data["OPENAI_API_KEY"]))
	})

	t.Run("create conflict", func(t *testing.T) {
		service, _, ctx := newService(&authimpl.NoopAuthorizer{}, &v1alpha2.ModelConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "default"},
			Spec:       v1alpha2.ModelConfigSpec{Model: "gpt-4", Provider: v1alpha2.ModelProviderOpenAI},
		})

		_, err := service.Create(ctx, model.CreateRequest{
			Ref:  "default/cfg",
			Spec: v1alpha2.ModelConfigSpec{Model: "gpt-4", Provider: v1alpha2.ModelProviderOpenAI},
		})
		require.Error(t, err)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeAlreadyExists))
	})

	t.Run("create invalid secret material", func(t *testing.T) {
		service, _, ctx := newService(&authimpl.NoopAuthorizer{})

		_, err := service.Create(ctx, model.CreateRequest{
			Ref: "default/cfg",
			Secrets: []secretmaterial.Material{{
				Name:  "Invalid_Name",
				Key:   "sa.json",
				Value: "{}",
			}},
			Spec: v1alpha2.ModelConfigSpec{Model: "gpt-4", Provider: v1alpha2.ModelProviderOpenAI},
		})
		require.Error(t, err)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
	})

	t.Run("create companion secret rollback", func(t *testing.T) {
		existingSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "provider-credentials", Namespace: "default"},
			Type:       corev1.SecretTypeOpaque,
			Data:       map[string][]byte{"credentials.json": []byte("original")},
		}
		service, kubeClient, ctx := newService(&authimpl.NoopAuthorizer{}, existingSecret)

		_, err := service.Create(ctx, model.CreateRequest{
			Ref: "default/test-config",
			Secrets: []secretmaterial.Material{{
				Name:  "provider-credentials",
				Key:   "credentials.json",
				Value: `{"token":"secret"}`,
			}},
			Spec: v1alpha2.ModelConfigSpec{
				Model:           "gpt-4",
				Provider:        v1alpha2.ModelProviderOpenAI,
				APIKeySecret:    "provider-credentials",
				APIKeySecretKey: "credentials.json",
			},
		})
		require.Error(t, err)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))

		config := &v1alpha2.ModelConfig{}
		err = kubeClient.Get(ctx, ctrlclient.ObjectKey{Namespace: "default", Name: "test-config"}, config)
		assert.Error(t, err)
		secret := &corev1.Secret{}
		err = kubeClient.Get(ctx, ctrlclient.ObjectKey{Namespace: "default", Name: "provider-credentials"}, secret)
		require.NoError(t, err)
		assert.Equal(t, "original", string(secret.Data["credentials.json"]))
	})

	t.Run("update writes secrets and sweeps stale refs", func(t *testing.T) {
		config := &v1alpha2.ModelConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "default", UID: types.UID("cfg-uid")},
			Spec: v1alpha2.ModelConfigSpec{
				Model:    "gpt-4",
				Provider: v1alpha2.ModelProviderOpenAI,
				TLS:      &v1alpha2.TLSConfig{CACertSecretRef: "ca-v1", CACertSecretKey: "ca.crt"},
			},
		}
		oldSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ca-v1",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: v1alpha2.GroupVersion.Identifier(),
					Kind:       "ModelConfig",
					Name:       "cfg",
					UID:        types.UID("cfg-uid"),
				}},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{"ca.crt": []byte("OLD")},
		}
		service, kubeClient, ctx := newService(&authimpl.NoopAuthorizer{}, config, oldSecret)

		updated, err := service.Update(ctx, model.UpdateRequest{
			Ref: types.NamespacedName{Namespace: "default", Name: "cfg"},
			Spec: v1alpha2.ModelConfigSpec{
				Model:    "gpt-4.1",
				Provider: v1alpha2.ModelProviderOpenAI,
				TLS:      &v1alpha2.TLSConfig{CACertSecretRef: "ca-v2", CACertSecretKey: "ca.crt"},
			},
			Secrets: []secretmaterial.Material{{Name: "ca-v2", Key: "ca.crt", Value: "NEW"}},
		})
		require.NoError(t, err)
		assert.Equal(t, "gpt-4.1", updated.Spec.Model)

		newSecret := &corev1.Secret{}
		err = kubeClient.Get(ctx, ctrlclient.ObjectKey{Namespace: "default", Name: "ca-v2"}, newSecret)
		require.NoError(t, err)
		assert.Equal(t, "NEW", string(newSecret.Data["ca.crt"]))

		deleted := &corev1.Secret{}
		err = kubeClient.Get(ctx, ctrlclient.ObjectKey{Namespace: "default", Name: "ca-v1"}, deleted)
		assert.Error(t, err)
	})

	t.Run("get not found", func(t *testing.T) {
		service, _, ctx := newService(&authimpl.NoopAuthorizer{})

		_, err := service.Get(ctx, model.GetRequest{Ref: types.NamespacedName{Namespace: "default", Name: "missing"}})
		require.Error(t, err)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	})

	t.Run("delete", func(t *testing.T) {
		config := &v1alpha2.ModelConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "default"},
			Spec:       v1alpha2.ModelConfigSpec{Model: "gpt-4", Provider: v1alpha2.ModelProviderOpenAI},
		}
		service, kubeClient, ctx := newService(&authimpl.NoopAuthorizer{}, config)

		deleted, err := service.Delete(ctx, model.DeleteRequest{Ref: types.NamespacedName{Namespace: "default", Name: "cfg"}})
		require.NoError(t, err)
		assert.Equal(t, "cfg", deleted.Name)

		fetched := &v1alpha2.ModelConfig{}
		err = kubeClient.Get(ctx, ctrlclient.ObjectKey{Namespace: "default", Name: "cfg"}, fetched)
		assert.Error(t, err)
	})

	t.Run("permission denied", func(t *testing.T) {
		service, _, ctx := newService(denyAuthorizer{})

		_, err := service.List(ctx, model.ListRequest{})
		require.Error(t, err)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
	})
}
