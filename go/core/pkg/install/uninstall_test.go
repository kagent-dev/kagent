package install

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func TestUninstallReversesInstall(t *testing.T) {
	const (
		listReleases = "helm list --all-namespaces --deployed --failed --pending --uninstalling --filter ^kagent(-crds)?$ --output json"
		listPools    = "kubectl get workerpools.ate.dev --all-namespaces -l app.kubernetes.io/instance!=kagent -o jsonpath={range .items[*]}{.metadata.namespace}/{.metadata.name} {end}"
	)
	before := []string{
		listReleases,
		listPools,
		"kubectl delete sandboxtemplates.api.kagent.dev --all --all-namespaces --ignore-not-found --timeout=5m",
		"helm uninstall kagent --namespace kagent --wait",
		"kubectl delete sandboxconfigs.ate.dev --all --ignore-not-found --timeout=5m",
		"helm uninstall substrate --namespace ate-system --wait",
		"kubectl -n kagent get deployment/kagent-postgresql --ignore-not-found -o name",
	}
	after := []string{
		"helm uninstall substrate-podcert --namespace podcertificate-controller-system --wait",
		"helm uninstall substrate-crds --namespace ate-system --wait",
		"helm uninstall kagent-crds --namespace kagent --wait",
		`kubectl patch namespace kagent --type=merge -p {"metadata":{"labels":{"ate.dev/api-access":null}}}`,
		"kubectl delete namespace podcertificate-controller-system ate-system --ignore-not-found",
		"kubectl delete clustertrustbundles.certificates.k8s.io " + strings.Join(substrateTrustBundles, " ") + " --ignore-not-found --timeout=5m",
	}
	for _, tc := range []struct {
		name, releases, deployment, pools, wantErr string
		alreadyGone                                bool
		want                                       []string
	}{
		{"bundled database", `[{"namespace":"kagent"}]`, "deployment.apps/kagent-postgresql", "", "", false,
			slices.Concat(before, []string{"kubectl delete --ignore-not-found -f -"}, after)},
		{"external database", `[{"namespace":"kagent"}]`, "", "", "", false, slices.Concat(before, after)},
		{"interrupted uninstall", `[]`, "", "", "", true, slices.Concat(before, after)},
		{"wrong namespace", `[{"namespace":"other"}]`, "", "", "namespace other, not kagent", false, []string{listReleases}},
		{"CRDs left in another namespace", `[{"name":"kagent-crds","namespace":"other"}]`, "", "", "namespace other, not kagent", false, []string{listReleases}},
		{"user worker pools", `[{"namespace":"kagent"}]`, "", "team/http default/batch ", "self-managed WorkerPools team/http, default/batch", false,
			[]string{listReleases, listPools}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &recordingRunner{run: func(cmd Command) (string, error) {
				args := strings.Join(cmd.Args, " ")
				switch {
				case cmd.Program == "helm" && cmd.Args[0] == "list":
					return tc.releases, nil
				case strings.Contains(args, "get deployment/kagent-postgresql"):
					return tc.deployment, nil
				case strings.HasPrefix(args, "get workerpools"):
					if tc.alreadyGone {
						return "", errors.New("the server doesn't have a resource type")
					}
					return tc.pools, nil
				case tc.alreadyGone && cmd.Program == "helm":
					return "", errors.New("release not found")
				case tc.alreadyGone && strings.HasPrefix(args, "delete sandbox"):
					return "", errors.New("the server doesn't have a resource type")
				case tc.alreadyGone && strings.HasPrefix(args, "patch namespace"):
					return "", errors.New("namespace not found")
				}
				return "", nil
			}}
			result, err := NewClient(runner, fake.NewSimpleClientset()).Uninstall(t.Context(), "kagent", "postgres:18-alpine", nil)
			if tc.pools != "" {
				var poolsErr *SelfManagedWorkerPoolsError
				require.ErrorAs(t, err, &poolsErr)
				require.Equal(t, strings.Fields(tc.pools), poolsErr.Pools)
			}
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			if tc.alreadyGone {
				require.Equal(t, []string{"kagent", "substrate", "substrate-podcert", "substrate-crds", "kagent-crds"}, result.MissingReleases)
			}
			var commands []string
			for _, cmd := range runner.commands {
				commands = append(commands, cmd.Program+" "+strings.Join(cmd.Args, " "))
				if cmd.Program == "kubectl" && len(cmd.Args) > 0 && cmd.Args[0] == "delete" && cmd.Stdin != "" {
					require.Contains(t, cmd.Stdin, "kind: PersistentVolumeClaim")
				}
			}
			require.Equal(t, tc.want, commands)
		})
	}
}

func TestUninstallStopsAtFailure(t *testing.T) {
	for _, failAt := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12} {
		t.Run(fmt.Sprintf("command_%d", failAt), func(t *testing.T) {
			failure := errors.New("cluster unavailable")
			calls := 0
			runner := &recordingRunner{run: func(cmd Command) (string, error) {
				calls++
				if calls == failAt+1 {
					return "", failure
				}
				if cmd.Program == "helm" && cmd.Args[0] == "list" {
					return "[]", nil
				}
				return "", nil
			}}
			_, err := NewClient(runner, fake.NewSimpleClientset()).Uninstall(t.Context(), "team", "postgres:18-alpine", nil)
			require.ErrorIs(t, err, failure)
			require.Equal(t, failAt+1, calls)
		})
	}
}

func TestDeleteWorkerPools(t *testing.T) {
	runner := &recordingRunner{}
	require.NoError(t, NewClient(runner, fake.NewSimpleClientset()).DeleteWorkerPools(t.Context(), []string{"team/http", "default/batch"}))
	var commands []string
	for _, cmd := range runner.commands {
		commands = append(commands, cmd.Program+" "+strings.Join(cmd.Args, " "))
	}
	require.Equal(t, []string{
		"kubectl delete workerpools.ate.dev --namespace team http --ignore-not-found --timeout=5m",
		"kubectl delete workerpools.ate.dev --namespace default batch --ignore-not-found --timeout=5m",
	}, commands)
}
