package kubecrud_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

func agents(refs ...[2]string) []*v1alpha3.Agent {
	items := make([]*v1alpha3.Agent, 0, len(refs))
	for _, ref := range refs {
		items = append(items, &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: ref[0], Name: ref[1]}})
	}
	return items
}

func names(items []*v1alpha3.Agent) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Namespace+"/"+item.Name)
	}
	return result
}

func TestPageWalksAcrossNamespaces(t *testing.T) {
	items := agents([2]string{"a", "x"}, [2]string{"a", "y"}, [2]string{"b", "x"}, [2]string{"c", "z"}, [2]string{"c", "zz"})

	var seen []string
	token := ""
	for pages := 0; ; pages++ {
		require.Less(t, pages, 5, "paging never ended")
		page, next, err := kubecrud.Page(items, 2, token)
		require.NoError(t, err)
		require.LessOrEqual(t, len(page), 2)
		seen = append(seen, names(page)...)
		if token = next; token == "" {
			break
		}
	}
	require.Equal(t, []string{"a/x", "a/y", "b/x", "c/z", "c/zz"}, seen)
}

// A caller that has never sent a page, such as an older UI, keeps getting the whole list.
func TestPageWithoutLimitReturnsEverything(t *testing.T) {
	items := agents([2]string{"a", "x"}, [2]string{"b", "y"})

	page, next, err := kubecrud.Page(items, 0, "")
	require.NoError(t, err)
	require.Len(t, page, 2)
	require.Empty(t, next)
}

// The cursor is a position, so deleting the last item of a page does not repeat or skip items.
func TestPageResumesAfterTheCursorItemIsDeleted(t *testing.T) {
	_, next, err := kubecrud.Page(agents([2]string{"a", "x"}, [2]string{"a", "y"}, [2]string{"b", "x"}), 2, "")
	require.NoError(t, err)

	page, _, err := kubecrud.Page(agents([2]string{"a", "x"}, [2]string{"b", "x"}), 2, next)
	require.NoError(t, err)
	require.Equal(t, []string{"b/x"}, names(page))
}

func TestPageRejectsInvalidTokens(t *testing.T) {
	for _, token := range []string{"not base64!", base64.RawURLEncoding.EncodeToString([]byte("no-slash")), base64.RawURLEncoding.EncodeToString([]byte("/name"))} {
		_, _, err := kubecrud.Page(agents([2]string{"a", "x"}), 1, token)
		require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), "token %q: %v", token, err)
	}
}
