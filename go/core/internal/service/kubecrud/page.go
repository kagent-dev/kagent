package kubecrud

import (
	"cmp"
	"encoding/base64"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

// Page returns the items after the item the token names, up to limit, and the
// token for the next page, which is empty on the last page. items must be in
// List's namespace-then-name order. A zero limit returns every remaining item,
// so a caller that has never sent a page keeps receiving the whole list.
//
// Paging works on an in-memory slice. The list comes from the controller's
// cache, which has no continue tokens, and List filters it after reading it,
// so every page still lists and sorts the whole collection. Fine at
// custom-resource scale; a database-backed store would page in its query.
func Page[T Object](items []T, limit int, token string) ([]T, string, error) {
	if token != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil {
			return nil, "", serviceerrors.NewInvalidArgument("Invalid page token", err)
		}
		namespace, name, ok := strings.Cut(string(decoded), "/")
		if !ok || namespace == "" || name == "" {
			return nil, "", serviceerrors.NewInvalidArgument("Invalid page token", nil)
		}
		// The cursor is a position, not an item, so a deleted cursor item still pages correctly.
		start, _ := slices.BinarySearchFunc(items, [2]string{namespace, name}, func(item T, cursor [2]string) int {
			return cmp.Or(cmp.Compare(item.GetNamespace(), cursor[0]), cmp.Compare(item.GetName(), cursor[1]))
		})
		if start < len(items) && items[start].GetNamespace() == namespace && items[start].GetName() == name {
			start++
		}
		items = items[start:]
	}
	if limit <= 0 || len(items) <= limit {
		return items, "", nil
	}
	last := items[limit-1]
	return items[:limit], base64.RawURLEncoding.EncodeToString([]byte(last.GetNamespace() + "/" + last.GetName())), nil
}
