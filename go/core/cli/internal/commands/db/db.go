// Package db wires the shared database subcommand to the CLI's migration tracks.
package db

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	dbcli "github.com/kagent-dev/kagent/go/core/cli/internal/db"
	dbmigrate "github.com/kagent-dev/kagent/go/core/cli/internal/db/migrate"
	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// vectorEnabledKey names two lookups that deliberately share it: the CLI's
// own DATABASE_VECTOR_ENABLED env var (a local operator override), and the
// controller-configmap key the chart renders — the value the controller pod
// itself consumes via envFrom. Same name, two different places.
var vectorEnabledKey = kagentenv.DatabaseVectorEnabled.Name()

// NewDBCmd constructs the kagent db command. The source callback the shared db
// package takes carries no command, so the namespace is captured just before a
// subcommand runs, when the root's flags have been parsed.
func NewDBCmd() *cobra.Command {
	var namespace string
	cmd := dbcli.NewCommandFromFunc(migrationSources(&namespace))
	cmd.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		options, err := connection.OptionsFromCommand(cmd)
		if err != nil {
			return err
		}
		namespace = options.Namespace
		return nil
	}
	return cmd
}

// migrationSources resolves the built-in migration tracks when a db
// subcommand runs (never during command construction, so unrelated commands
// do no work and print no warnings). The vector track is gated, in order of
// precedence, on: the DATABASE_VECTOR_ENABLED env var in the CLI's own
// environment (explicit operator intent, works without a cluster), the
// controller's configmap on the live cluster (the same value the server
// reads), and finally the controller's default (enabled).
func migrationSources(namespace *string) dbmigrate.SourcesFunc {
	return func(ctx context.Context) ([]migrations.Source, error) {
		vectorEnabled := true
		if v := os.Getenv(vectorEnabledKey); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: invalid %s=%q; assuming true\n", vectorEnabledKey, v)
			} else {
				vectorEnabled = b
			}
		} else if b, ok := clusterVectorEnabled(ctx, *namespace); ok {
			vectorEnabled = b
		}
		return migrations.BuiltinSources(vectorEnabled), nil
	}
}

// clusterVectorEnabled reads the vectorEnabledKey entry from the controller
// configmap in the given namespace (the same "kagent-controller" default
// naming the rest of the CLI assumes) — the cluster-side counterpart of the
// env-var override in migrationSources. When the value is used it says so on
// stderr, naming the kubeconfig context it was read from — the lookup follows
// the *current* context, so this is the operator's cue that the cluster and
// their --db-url had better be the same install. Best-effort: reports
// ok=false when no cluster is reachable, the configmap is absent, or the
// value doesn't parse — callers fall back to the default.
func clusterVectorEnabled(ctx context.Context, namespace string) (enabled, ok bool) {
	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return false, false
	}
	k8sClient, err := client.New(restConfig, client.Options{})
	if err != nil {
		return false, false
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var cm corev1.ConfigMap
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "kagent-controller"}, &cm); err != nil {
		return false, false
	}
	b, err := strconv.ParseBool(cm.Data[vectorEnabledKey])
	if err != nil {
		return false, false
	}
	// Trailing blank line separates the notice from the command's stdout
	// when both land on a terminal; piped stdout is unaffected.
	fmt.Fprintf(os.Stderr, "resolved vector track from cluster context %q: configmap %s/kagent-controller has %s=%t (set %s to override)\n\n",
		currentKubeContext(), namespace, vectorEnabledKey, b, vectorEnabledKey)
	return b, true
}

// currentKubeContext names the kubeconfig context the CLI's Kubernetes client
// dials, for operator-facing messages. Best-effort.
func currentKubeContext() string {
	raw, err := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	if err != nil || raw.CurrentContext == "" {
		return "(current kubeconfig context)"
	}
	return raw.CurrentContext
}
