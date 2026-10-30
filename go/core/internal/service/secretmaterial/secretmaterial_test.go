package secretmaterial_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/secretmaterial"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

const testNamespace = "default"

var testGVK = v1alpha3.GroupVersion.WithKind("ModelConfig")

func newOwner() *v1alpha3.ModelConfig {
	return &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: testNamespace, UID: types.UID("owner-uid")},
	}
}

// ownerRef is a reference that matches newOwner(); tests change one field at a time.
func ownerRef() metav1.OwnerReference {
	return secretmaterial.OwnerReferenceFor(newOwner(), testGVK)
}

func newSecret(name string, secretType corev1.SecretType, data map[string][]byte, refs ...metav1.OwnerReference) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace, OwnerReferences: refs},
		Type:       secretType,
		Data:       data,
	}
}

func newClient(t *testing.T, funcs interceptor.Funcs, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(funcs).Build()
}

func getSecret(t *testing.T, kubeClient client.Client, name string) *corev1.Secret {
	t.Helper()
	secret := &corev1.Secret{}
	require.NoError(t, kubeClient.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: name}, secret))
	return secret
}

func TestValidateMaterials(t *testing.T) {
	tests := []struct {
		name      string
		materials []secretmaterial.Material
		wantErr   string
	}{
		{name: "no materials", materials: nil},
		{name: "valid", materials: []secretmaterial.Material{{Name: "my-secret", Key: "api_key.json", Value: "v"}}},
		{name: "empty name", materials: []secretmaterial.Material{{Name: "", Key: "key"}}, wantErr: "invalid secret name"},
		{name: "uppercase name", materials: []secretmaterial.Material{{Name: "My-Secret", Key: "key"}}, wantErr: "invalid secret name"},
		{name: "underscore in name", materials: []secretmaterial.Material{{Name: "my_secret", Key: "key"}}, wantErr: "invalid secret name"},
		{name: "name too long", materials: []secretmaterial.Material{{Name: strings.Repeat("a", 254), Key: "key"}}, wantErr: "invalid secret name"},
		{name: "empty key", materials: []secretmaterial.Material{{Name: "s", Key: ""}}, wantErr: "invalid key"},
		{name: "key with space", materials: []secretmaterial.Material{{Name: "s", Key: "bad key"}}, wantErr: "invalid key"},
		{name: "key is dot-dot", materials: []secretmaterial.Material{{Name: "s", Key: ".."}}, wantErr: "invalid key"},
		{name: "key too long", materials: []secretmaterial.Material{{Name: "s", Key: strings.Repeat("k", 254)}}, wantErr: "invalid key"},
		{
			name: "a later invalid material is still reported",
			materials: []secretmaterial.Material{
				{Name: "good", Key: "key"},
				{Name: "bad_name", Key: "key"},
			},
			wantErr: `invalid secret name "bad_name"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := secretmaterial.ValidateMaterials(tt.materials)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
			assert.Contains(t, serviceerrors.MessageOf(err), tt.wantErr)
		})
	}
}

func TestOwnerReferenceFor(t *testing.T) {
	tests := []struct {
		name           string
		gvk            schema.GroupVersionKind
		wantAPIVersion string
	}{
		{name: "named group", gvk: testGVK, wantAPIVersion: "api.kagent.dev/v1alpha3"},
		{name: "core group has no prefix", gvk: schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, wantAPIVersion: "v1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := secretmaterial.OwnerReferenceFor(newOwner(), tt.gvk)
			assert.Equal(t, tt.wantAPIVersion, ref.APIVersion)
			assert.Equal(t, tt.gvk.Kind, ref.Kind)
			assert.Equal(t, "owner", ref.Name)
			assert.Equal(t, types.UID("owner-uid"), ref.UID)
			require.NotNil(t, ref.Controller)
			assert.True(t, *ref.Controller)
		})
	}
}

func TestIsOwnedBy(t *testing.T) {
	withChange := func(change func(*metav1.OwnerReference)) metav1.OwnerReference {
		ref := ownerRef()
		change(&ref)
		return ref
	}
	noUID := newOwner()
	noUID.UID = ""

	tests := []struct {
		name   string
		secret *corev1.Secret
		owner  client.Object
		want   bool
	}{
		{name: "matching reference", secret: newSecret("s", corev1.SecretTypeOpaque, nil, ownerRef()), owner: newOwner(), want: true},
		{name: "no references", secret: newSecret("s", corev1.SecretTypeOpaque, nil), owner: newOwner(), want: false},
		{
			name:   "different apiVersion",
			secret: newSecret("s", corev1.SecretTypeOpaque, nil, withChange(func(r *metav1.OwnerReference) { r.APIVersion = "api.kagent.dev/v1alpha2" })),
			owner:  newOwner(),
		},
		{
			name:   "different kind",
			secret: newSecret("s", corev1.SecretTypeOpaque, nil, withChange(func(r *metav1.OwnerReference) { r.Kind = "RemoteMCPServer" })),
			owner:  newOwner(),
		},
		{
			name:   "different name",
			secret: newSecret("s", corev1.SecretTypeOpaque, nil, withChange(func(r *metav1.OwnerReference) { r.Name = "other" })),
			owner:  newOwner(),
		},
		{
			// A recreated owner reuses the name with a new UID; the old Secret is not its own.
			name:   "different UID",
			secret: newSecret("s", corev1.SecretTypeOpaque, nil, withChange(func(r *metav1.OwnerReference) { r.UID = "old-uid" })),
			owner:  newOwner(),
		},
		{
			// An owner that was never persisted has no UID; matching it by name alone would be unsafe.
			name:   "owner without UID never matches",
			secret: newSecret("s", corev1.SecretTypeOpaque, nil, withChange(func(r *metav1.OwnerReference) { r.UID = "" })),
			owner:  noUID,
		},
		{
			name: "matching reference among others",
			secret: newSecret("s", corev1.SecretTypeOpaque, nil,
				withChange(func(r *metav1.OwnerReference) { r.Name = "other" }),
				ownerRef(),
			),
			owner: newOwner(),
			want:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, secretmaterial.IsOwnedBy(tt.secret, tt.owner, testGVK))
		})
	}
}

func TestCreateCompanionSecrets(t *testing.T) {
	ctx := context.Background()

	t.Run("creates one owned Opaque secret per name with all of its keys", func(t *testing.T) {
		kubeClient := newClient(t, interceptor.Funcs{})
		err := secretmaterial.CreateCompanionSecrets(ctx, kubeClient, newOwner(), testGVK, []secretmaterial.Material{
			{Name: "auth", Key: "token", Value: "t"},
			{Name: "auth", Key: "refresh", Value: "r"},
			{Name: "tls", Key: "ca", Value: "c"},
		})
		require.NoError(t, err)

		auth := getSecret(t, kubeClient, "auth")
		assert.Equal(t, corev1.SecretTypeOpaque, auth.Type)
		assert.Equal(t, map[string][]byte{"token": []byte("t"), "refresh": []byte("r")}, auth.Data)
		assert.True(t, secretmaterial.IsOwnedBy(auth, newOwner(), testGVK))
		assert.Equal(t, map[string][]byte{"ca": []byte("c")}, getSecret(t, kubeClient, "tls").Data)
	})

	t.Run("merges into an existing owned secret and keeps its other keys", func(t *testing.T) {
		existing := newSecret("auth", corev1.SecretTypeOpaque, map[string][]byte{"keep": []byte("k"), "token": []byte("old")}, ownerRef())
		kubeClient := newClient(t, interceptor.Funcs{}, existing)

		err := secretmaterial.CreateCompanionSecrets(ctx, kubeClient, newOwner(), testGVK, []secretmaterial.Material{
			{Name: "auth", Key: "token", Value: "new"},
		})
		require.NoError(t, err)
		assert.Equal(t, map[string][]byte{"keep": []byte("k"), "token": []byte("new")}, getSecret(t, kubeClient, "auth").Data)
	})

	t.Run("writes into an existing owned secret that has no data", func(t *testing.T) {
		kubeClient := newClient(t, interceptor.Funcs{}, newSecret("auth", corev1.SecretTypeOpaque, nil, ownerRef()))

		err := secretmaterial.CreateCompanionSecrets(ctx, kubeClient, newOwner(), testGVK, []secretmaterial.Material{
			{Name: "auth", Key: "token", Value: "t"},
		})
		require.NoError(t, err)
		assert.Equal(t, map[string][]byte{"token": []byte("t")}, getSecret(t, kubeClient, "auth").Data)
	})

	t.Run("rejects an existing secret that is not Opaque and leaves it unchanged", func(t *testing.T) {
		existing := newSecret("auth", corev1.SecretTypeBasicAuth, map[string][]byte{"username": []byte("u")}, ownerRef())
		kubeClient := newClient(t, interceptor.Funcs{}, existing)

		err := secretmaterial.CreateCompanionSecrets(ctx, kubeClient, newOwner(), testGVK, []secretmaterial.Material{
			{Name: "auth", Key: "token", Value: "t"},
		})
		require.Error(t, err)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
		assert.Contains(t, serviceerrors.MessageOf(err), "must be type")
		assert.Equal(t, map[string][]byte{"username": []byte("u")}, getSecret(t, kubeClient, "auth").Data)
	})

	t.Run("rejects an existing secret owned by someone else and leaves it unchanged", func(t *testing.T) {
		existing := newSecret("auth", corev1.SecretTypeOpaque, map[string][]byte{"token": []byte("theirs")})
		kubeClient := newClient(t, interceptor.Funcs{}, existing)

		err := secretmaterial.CreateCompanionSecrets(ctx, kubeClient, newOwner(), testGVK, []secretmaterial.Material{
			{Name: "auth", Key: "token", Value: "mine"},
		})
		require.Error(t, err)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
		assert.Contains(t, serviceerrors.MessageOf(err), "is not managed by")
		assert.Equal(t, map[string][]byte{"token": []byte("theirs")}, getSecret(t, kubeClient, "auth").Data)
	})

	failures := []struct {
		name    string
		funcs   interceptor.Funcs
		objects []client.Object
	}{
		{
			name: "get failure",
			funcs: interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return errors.New("boom")
			}},
		},
		{
			name: "create failure",
			funcs: interceptor.Funcs{Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
				return errors.New("boom")
			}},
		},
		{
			name: "update failure",
			funcs: interceptor.Funcs{Update: func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
				return errors.New("boom")
			}},
			objects: []client.Object{newSecret("auth", corev1.SecretTypeOpaque, nil, ownerRef())},
		},
	}
	for _, tt := range failures {
		t.Run(tt.name+" is reported as internal", func(t *testing.T) {
			kubeClient := newClient(t, tt.funcs, tt.objects...)
			err := secretmaterial.CreateCompanionSecrets(ctx, kubeClient, newOwner(), testGVK, []secretmaterial.Material{
				{Name: "auth", Key: "token", Value: "t"},
			})
			require.Error(t, err)
			assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInternal))
			assert.ErrorContains(t, err, "boom")
		})
	}
}

func TestCreateOwnedOpaqueSecret(t *testing.T) {
	ctx := context.Background()

	t.Run("creates an owned Opaque secret in the owner's namespace", func(t *testing.T) {
		kubeClient := newClient(t, interceptor.Funcs{})
		require.NoError(t, secretmaterial.CreateOwnedOpaqueSecret(ctx, kubeClient, newOwner(), testGVK, "key", map[string]string{"a": "1", "b": "2"}))

		secret := getSecret(t, kubeClient, "key")
		assert.Equal(t, corev1.SecretTypeOpaque, secret.Type)
		assert.Equal(t, map[string][]byte{"a": []byte("1"), "b": []byte("2")}, secret.Data)
		assert.True(t, secretmaterial.IsOwnedBy(secret, newOwner(), testGVK))
	})

	t.Run("does not overwrite an existing secret", func(t *testing.T) {
		kubeClient := newClient(t, interceptor.Funcs{}, newSecret("key", corev1.SecretTypeOpaque, map[string][]byte{"a": []byte("theirs")}))

		err := secretmaterial.CreateOwnedOpaqueSecret(ctx, kubeClient, newOwner(), testGVK, "key", map[string]string{"a": "mine"})
		require.Error(t, err)
		assert.True(t, apierrors.IsAlreadyExists(err), "want AlreadyExists, got %v", err)
		assert.Equal(t, map[string][]byte{"a": []byte("theirs")}, getSecret(t, kubeClient, "key").Data)
	})
}

func TestCreateOrUpdateOwnedOpaqueSecret(t *testing.T) {
	ctx := context.Background()

	t.Run("creates an owned Opaque secret when none exists", func(t *testing.T) {
		kubeClient := newClient(t, interceptor.Funcs{})
		require.NoError(t, secretmaterial.CreateOrUpdateOwnedOpaqueSecret(ctx, kubeClient, newOwner(), testGVK, "key", map[string]string{"OPENAI_API_KEY": "sk"}))

		secret := getSecret(t, kubeClient, "key")
		assert.Equal(t, corev1.SecretTypeOpaque, secret.Type)
		assert.Equal(t, map[string][]byte{"OPENAI_API_KEY": []byte("sk")}, secret.Data)
		assert.True(t, secretmaterial.IsOwnedBy(secret, newOwner(), testGVK))
	})

	t.Run("updates the given keys of an existing secret and keeps the rest", func(t *testing.T) {
		existing := newSecret("key", corev1.SecretTypeOpaque, map[string][]byte{"keep": []byte("k"), "OPENAI_API_KEY": []byte("old")}, ownerRef())
		kubeClient := newClient(t, interceptor.Funcs{}, existing)

		require.NoError(t, secretmaterial.CreateOrUpdateOwnedOpaqueSecret(ctx, kubeClient, newOwner(), testGVK, "key", map[string]string{"OPENAI_API_KEY": "new"}))
		assert.Equal(t, map[string][]byte{"keep": []byte("k"), "OPENAI_API_KEY": []byte("new")}, getSecret(t, kubeClient, "key").Data)
	})

	t.Run("updates an existing secret that has no data", func(t *testing.T) {
		kubeClient := newClient(t, interceptor.Funcs{}, newSecret("key", corev1.SecretTypeOpaque, nil, ownerRef()))

		require.NoError(t, secretmaterial.CreateOrUpdateOwnedOpaqueSecret(ctx, kubeClient, newOwner(), testGVK, "key", map[string]string{"k": "v"}))
		assert.Equal(t, map[string][]byte{"k": []byte("v")}, getSecret(t, kubeClient, "key").Data)
	})

	t.Run("returns a get failure that is not NotFound", func(t *testing.T) {
		injected := errors.New("boom")
		kubeClient := newClient(t, interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			return injected
		}})
		err := secretmaterial.CreateOrUpdateOwnedOpaqueSecret(ctx, kubeClient, newOwner(), testGVK, "key", map[string]string{"k": "v"})
		require.ErrorIs(t, err, injected)
	})

	t.Run("returns an update failure", func(t *testing.T) {
		injected := errors.New("boom")
		kubeClient := newClient(t, interceptor.Funcs{Update: func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
			return injected
		}}, newSecret("key", corev1.SecretTypeOpaque, nil, ownerRef()))
		err := secretmaterial.CreateOrUpdateOwnedOpaqueSecret(ctx, kubeClient, newOwner(), testGVK, "key", map[string]string{"k": "v"})
		require.ErrorIs(t, err, injected)
	})
}

func TestDeleteOwnedSecret(t *testing.T) {
	ctx := context.Background()
	notFound := apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "s")

	t.Run("deletes a secret the owner owns", func(t *testing.T) {
		kubeClient := newClient(t, interceptor.Funcs{}, newSecret("s", corev1.SecretTypeOpaque, nil, ownerRef()))
		require.NoError(t, secretmaterial.DeleteOwnedSecret(ctx, kubeClient, newOwner(), testGVK, "s"))

		err := kubeClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "s"}, &corev1.Secret{})
		assert.True(t, apierrors.IsNotFound(err), "secret should be gone, got %v", err)
	})

	t.Run("leaves a secret the owner does not own", func(t *testing.T) {
		kubeClient := newClient(t, interceptor.Funcs{}, newSecret("s", corev1.SecretTypeOpaque, map[string][]byte{"k": []byte("v")}))
		require.NoError(t, secretmaterial.DeleteOwnedSecret(ctx, kubeClient, newOwner(), testGVK, "s"))
		assert.Equal(t, map[string][]byte{"k": []byte("v")}, getSecret(t, kubeClient, "s").Data)
	})

	t.Run("a missing secret is not an error", func(t *testing.T) {
		kubeClient := newClient(t, interceptor.Funcs{})
		require.NoError(t, secretmaterial.DeleteOwnedSecret(ctx, kubeClient, newOwner(), testGVK, "s"))
	})

	t.Run("returns a get failure that is not NotFound", func(t *testing.T) {
		injected := errors.New("boom")
		kubeClient := newClient(t, interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			return injected
		}})
		require.ErrorIs(t, secretmaterial.DeleteOwnedSecret(ctx, kubeClient, newOwner(), testGVK, "s"), injected)
	})

	t.Run("returns a delete failure", func(t *testing.T) {
		injected := errors.New("boom")
		kubeClient := newClient(t, interceptor.Funcs{Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
			return injected
		}}, newSecret("s", corev1.SecretTypeOpaque, nil, ownerRef()))
		require.ErrorIs(t, secretmaterial.DeleteOwnedSecret(ctx, kubeClient, newOwner(), testGVK, "s"), injected)
	})

	t.Run("a secret that disappears during delete is not an error", func(t *testing.T) {
		kubeClient := newClient(t, interceptor.Funcs{Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
			return notFound
		}}, newSecret("s", corev1.SecretTypeOpaque, nil, ownerRef()))
		require.NoError(t, secretmaterial.DeleteOwnedSecret(ctx, kubeClient, newOwner(), testGVK, "s"))
	})
}

func TestRollbackOwnerOnCreateFailure(t *testing.T) {
	ctx := context.Background()

	t.Run("deletes the owner", func(t *testing.T) {
		owner := newOwner()
		kubeClient := newClient(t, interceptor.Funcs{}, owner.DeepCopy())
		require.NoError(t, secretmaterial.RollbackOwnerOnCreateFailure(ctx, kubeClient, owner))

		err := kubeClient.Get(ctx, client.ObjectKeyFromObject(owner), &v1alpha3.ModelConfig{})
		assert.True(t, apierrors.IsNotFound(err), "owner should be gone, got %v", err)
	})

	t.Run("an owner that is already gone is not an error", func(t *testing.T) {
		kubeClient := newClient(t, interceptor.Funcs{})
		require.NoError(t, secretmaterial.RollbackOwnerOnCreateFailure(ctx, kubeClient, newOwner()))
	})

	t.Run("returns a delete failure", func(t *testing.T) {
		injected := errors.New("boom")
		kubeClient := newClient(t, interceptor.Funcs{Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
			return injected
		}}, newOwner())
		require.ErrorIs(t, secretmaterial.RollbackOwnerOnCreateFailure(ctx, kubeClient, newOwner()), injected)
	})
}
