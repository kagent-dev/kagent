// Copyright 2026 The Kagent Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package commands

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"os/exec"
	"strings"

	"github.com/agent-substrate/substrate/pkg/postgressetup"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"k8s.io/apimachinery/pkg/util/validation"
)

const substrateNamespace = "ate-system"

const bundledPostgresNamespaceYAML = `apiVersion: v1
kind: Namespace
metadata:
  name: ${NAMESPACE}
`

//go:embed bundled_postgres.yaml
var bundledPostgresYAML string

//go:embed bundled_postgres.sql
var bundledPostgresSQL string

func bundledPostgresSetupSQL() string {
	return "BEGIN;\n" + bundledPostgresSQL + "\n" + postgressetup.SQL() + "\nCOMMIT;\n"
}

// bundledPostgresManifest renders the PostgreSQL manifest kagent install applies. Both values are
// substituted into YAML, so they are validated rather than escaped.
func bundledPostgresManifest(namespace, image string) (string, error) {
	if problems := validation.IsDNS1123Label(namespace); len(problems) != 0 {
		return "", fmt.Errorf("invalid namespace %q: %s", namespace, strings.Join(problems, ", "))
	}
	if _, err := name.ParseReference(image); err != nil {
		return "", fmt.Errorf("invalid PostgreSQL image %q: %w", image, err)
	}
	return strings.NewReplacer(
		"${NAMESPACE}", namespace,
		"${SUBSTRATE_NAMESPACE}", substrateNamespace,
		"${POSTGRES_IMAGE}", image,
	).Replace(bundledPostgresYAML), nil
}

func kubectl(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("kubectl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func prepareBundledPostgres(ctx context.Context, namespace string) error {
	image := cmp.Or(env.KagentBundledPostgresImage.Get(), env.KagentBundledPostgresImage.DefaultValue())
	manifest, err := bundledPostgresManifest(namespace, image)
	if err != nil {
		return err
	}
	for _, targetNamespace := range []string{namespace, substrateNamespace} {
		ns := strings.ReplaceAll(bundledPostgresNamespaceYAML, "${NAMESPACE}", targetNamespace)
		if _, err := kubectl(ctx, ns, "apply", "--server-side", "--field-manager=kagent-cli", "-f", "-"); err != nil {
			return fmt.Errorf("create namespace %q: %w", targetNamespace, err)
		}
	}

	if _, err := kubectl(ctx, manifest, "apply", "--server-side", "--field-manager=kagent-cli", "-f", "-"); err != nil {
		return fmt.Errorf("deploy bundled PostgreSQL: %w", err)
	}
	if _, err := kubectl(ctx, "", "-n", namespace, "rollout", "status", "deployment/kagent-postgresql", "--timeout=5m"); err != nil {
		return fmt.Errorf("wait for bundled PostgreSQL: %w", err)
	}

	var sqlErr bytes.Buffer
	args := []string{"-n", namespace, "exec", "-i", "deployment/kagent-postgresql", "-c", "postgresql", "--",
		"psql", "--no-psqlrc", "--set=ON_ERROR_STOP=1", "--username", "postgres", "--dbname", "kagent"}
	cfg := postgressetup.DefaultConfig()
	// The bundled database authenticates application logins with client certificates.
	args = append(args, cfg.PSQLArgs()...)
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Stdin = strings.NewReader(bundledPostgresSetupSQL())
	cmd.Stderr = &sqlErr
	if err := cmd.Run(); err != nil {
		if detail := strings.TrimSpace(sqlErr.String()); detail != "" {
			return fmt.Errorf("prepare bundled PostgreSQL: %w: %s", err, detail)
		}
		return fmt.Errorf("prepare bundled PostgreSQL: %w", err)
	}
	return nil
}
