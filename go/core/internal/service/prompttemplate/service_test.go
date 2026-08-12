package prompttemplate_test

import (
	"context"
	"errors"
	"testing"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/prompttemplate"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type denyAuthorizer struct{}

func (denyAuthorizer) Check(context.Context, pkgauth.Principal, pkgauth.Verb, pkgauth.Resource) error {
	return errors.New("denied")
}

func TestServiceCRUDAndValidation(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	newService := func(authorizer pkgauth.Authorizer, objects ...ctrlclient.Object) (*prompttemplate.Service, ctrlclient.Client, context.Context) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
		service := prompttemplate.NewService(kubeClient, authorizer)
		ctx := pkgauth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{
			P: pkgauth.Principal{User: pkgauth.User{ID: "prompt-user"}},
		})
		return service, kubeClient, ctx
	}

	t.Run("list filters labels and sorts summaries", func(t *testing.T) {
		service, _, ctx := newService(&authimpl.NoopAuthorizer{},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "z-last", Labels: map[string]string{"kagent.dev/prompt-library": "true"}},
				Data:       map[string]string{"z": "last", "a": "first"},
				BinaryData: map[string][]byte{"binary": []byte("ignored")},
			},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "a-first", Labels: map[string]string{"kagent.dev/prompt-library": "true"}},
				Data:       map[string]string{"intro": "hello"},
			},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "noise"},
				Data:       map[string]string{"ignored": "true"},
			},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: "other", Labels: map[string]string{"kagent.dev/prompt-library": "true"}},
			},
		)

		result, err := service.List(ctx, "team")
		require.NoError(t, err)
		assert.Equal(t, []prompttemplate.Summary{
			{Namespace: "team", Name: "a-first", KeyCount: 1, Keys: []string{"intro"}},
			{Namespace: "team", Name: "z-last", KeyCount: 3, Keys: []string{"a", "z"}},
		}, result)

		_, err = service.List(ctx, "")
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)
	})

	t.Run("create get update and delete", func(t *testing.T) {
		service, kubeClient, ctx := newService(&authimpl.NoopAuthorizer{})
		created, err := service.Create(ctx, prompttemplate.CreateRequest{
			Namespace: "team",
			Name:      "library",
			Data:      map[string]string{"intro": "hello", "rules": "be concise"},
		})
		require.NoError(t, err)
		assert.Equal(t, "hello", created.Data["intro"])

		stored := &corev1.ConfigMap{}
		require.NoError(t, kubeClient.Get(ctx, types.NamespacedName{Namespace: "team", Name: "library"}, stored))
		assert.Equal(t, "true", stored.Labels["kagent.dev/prompt-library"])

		got, err := service.Get(ctx, types.NamespacedName{Namespace: "team", Name: "library"})
		require.NoError(t, err)
		assert.Equal(t, created, got)

		updated, err := service.Update(ctx, types.NamespacedName{Namespace: "team", Name: "library"}, map[string]string{"new": "replacement"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"new": "replacement"}, updated.Data)

		require.NoError(t, service.Delete(ctx, types.NamespacedName{Namespace: "team", Name: "library"}))
		err = kubeClient.Get(ctx, types.NamespacedName{Namespace: "team", Name: "library"}, &corev1.ConfigMap{})
		assert.True(t, apierrors.IsNotFound(err), err)
	})

	t.Run("canonical errors", func(t *testing.T) {
		existing := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "library"}}
		service, _, ctx := newService(&authimpl.NoopAuthorizer{}, existing)

		_, err := service.Create(ctx, prompttemplate.CreateRequest{Namespace: "team", Name: "library", Data: map[string]string{"key": "value"}})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeAlreadyExists), err)

		_, err = service.Get(ctx, types.NamespacedName{Namespace: "team", Name: "missing"})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound), err)

		_, err = service.Update(ctx, types.NamespacedName{Namespace: "team", Name: "missing"}, map[string]string{"key": "value"})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound), err)

		err = service.Delete(ctx, types.NamespacedName{Namespace: "team", Name: "missing"})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound), err)
	})

	t.Run("validates create and update inputs", func(t *testing.T) {
		service, _, ctx := newService(&authimpl.NoopAuthorizer{})
		createTests := []prompttemplate.CreateRequest{
			{Name: "library", Data: map[string]string{"key": "value"}},
			{Namespace: "INVALID", Name: "library", Data: map[string]string{"key": "value"}},
			{Namespace: "team", Data: map[string]string{"key": "value"}},
			{Namespace: "team", Name: "INVALID", Data: map[string]string{"key": "value"}},
			{Namespace: "team", Name: "library"},
			{Namespace: "team", Name: "library", Data: map[string]string{"": "value"}},
		}
		for _, request := range createTests {
			_, err := service.Create(ctx, request)
			assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)
		}

		_, err := service.Update(ctx, types.NamespacedName{Namespace: "team", Name: "library"}, nil)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)
		_, err = service.Get(ctx, types.NamespacedName{Name: "library"})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)
	})

	t.Run("authorization and authentication", func(t *testing.T) {
		service, _, ctx := newService(denyAuthorizer{})
		_, err := service.List(ctx, "team")
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), err)

		service, _, _ = newService(&authimpl.NoopAuthorizer{})
		_, err = service.List(context.Background(), "team")
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnauthenticated), err)
	})
}
