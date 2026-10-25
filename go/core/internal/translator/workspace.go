// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package translator

import (
	"fmt"
	"net/url"
	"strings"

	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/pkg/workspace"
)

// CompileWorkspace resolves the selected source and its preparation-only access.
// Secret values never enter the compiled inputs.
func CompileWorkspace(ctx krt.HandlerContext, secrets krt.Collection[*corev1.Secret], namespace string, selected *v1alpha3.WorkspaceSpec) (*configv1.GitWorkspaceConfig, egress.Access, error) {
	if selected == nil || selected.Git == nil {
		return nil, egress.Access{}, nil
	}
	source := selected.Git
	config := &configv1.GitWorkspaceConfig{Url: source.URL, Ref: source.Ref, Authorization: source.CredentialRef != nil}
	if source.RefreshToken != nil {
		config.RefreshToken = *source.RefreshToken
	}
	if err := workspace.Validate(config); err != nil {
		return nil, egress.Access{}, NewValidationError("invalid workspace: %v", err)
	}
	u, _ := url.Parse(config.Url) // validated above
	origin := egress.Origin(u)
	if strings.Contains(u.Hostname(), "*") {
		return nil, egress.Access{}, NewValidationError("workspace requires an exact HTTPS hostname")
	}
	var credentials []egress.Credential
	if ref := source.CredentialRef; ref != nil {
		if ref.Name == "" || ref.Key == "" || (ref.Optional != nil && *ref.Optional) {
			return nil, egress.Access{}, NewValidationError("workspace credentialRef requires a Secret name and key and cannot be optional")
		}
		secret := krt.FetchOne(ctx, secrets, krt.FilterKey(namespace+"/"+ref.Name))
		if secret == nil || len((*secret).Data[ref.Key]) == 0 {
			return nil, egress.Access{}, fmt.Errorf("workspace credential Secret %s/%s key %q is missing or empty", namespace, ref.Name, ref.Key)
		}
		credentials = []egress.Credential{{Origin: origin, Header: "authorization", URI: fmt.Sprintf("ate-secret://k8s.io/default/%s/%s/%s", namespace, ref.Name, ref.Key)}}
	}
	access, err := egress.CanonicalAccess([]string{origin}, credentials, nil)
	if err != nil {
		return nil, egress.Access{}, NewValidationError("invalid workspace access: %v", err)
	}
	return config, access, nil
}
