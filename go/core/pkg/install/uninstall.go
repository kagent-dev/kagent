package install

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// UninstallResult records releases that were already absent.
type UninstallResult struct{ MissingReleases []string }

// uninstallChart removes a Helm release and reports whether it existed, so a
// rerun after an interrupted uninstall skips the releases already gone.
func (c *Client) uninstallChart(ctx context.Context, release, namespace string) (bool, error) {
	out, err := c.runner.Run(ctx, Command{Program: "helm", Args: []string{"uninstall", release, "--namespace", namespace, "--wait"}})
	if err == nil {
		return true, nil
	}
	if strings.Contains(out+err.Error(), "not found") {
		return false, nil
	}
	return false, fmt.Errorf("failed to uninstall %s: %w: %s", release, err, strings.TrimSpace(out))
}

// kagentReleaseNamespaces includes CRD releases left by interrupted uninstalls.
// Explicit status filters cover pending releases on both Helm 3 and Helm 4.
func (c *Client) kagentReleaseNamespaces(ctx context.Context) ([]string, error) {
	out, err := c.runner.Run(ctx, Command{Program: "helm", Args: []string{"list", "--all-namespaces", "--deployed", "--failed", "--pending", "--uninstalling", "--filter", "^kagent(-crds)?$", "--output", "json"}})
	if err != nil {
		return nil, fmt.Errorf("failed to list kagent Helm releases: %w", err)
	}
	var releases []struct {
		Namespace string `json:"namespace"`
	}
	if err := json.Unmarshal([]byte(out), &releases); err != nil {
		return nil, fmt.Errorf("decode kagent Helm releases: %w", err)
	}
	namespaces := make([]string, 0, len(releases))
	for _, release := range releases {
		if !slices.Contains(namespaces, release.Namespace) {
			namespaces = append(namespaces, release.Namespace)
		}
	}
	return namespaces, nil
}

// deleteServed deletes objects and waits for their finalizers. A resource the
// cluster no longer serves has nothing to delete, as on a rerun after its CRD
// is gone.
func (c *Client) deleteServed(ctx context.Context, resource string, args ...string) error {
	out, err := c.kubectl(ctx, "", append(append([]string{"delete", resource}, args...), "--ignore-not-found", "--timeout=5m")...)
	if err != nil && !strings.Contains(out+err.Error(), "doesn't have a resource type") {
		return err
	}
	return nil
}

// substrateTrustBundles are the cluster-scoped ClusterTrustBundles Substrate's
// controllers publish. Kubernetes requires a bundle whose name has a signer
// prefix to carry that signer, so the names alone identify them.
var substrateTrustBundles = []string{
	"podidentity.podcert.ate.dev:identity:primary-bundle",
	"servicedns.podcert.ate.dev:identity:primary-bundle",
	"postgres.podcert.ate.dev:identity:primary-bundle",
	"egress-mitm.ate.dev:mitm:primary-bundle",
}

// Uninstall removes the shared releases and setup in dependency order. It keeps
// external databases and the kagent namespace, and tolerates interrupted uninstalls.
func (c *Client) Uninstall(ctx context.Context, namespace, postgresImage string, progress func(string)) (UninstallResult, error) {
	var result UninstallResult
	report := func(message string) {
		if progress != nil {
			progress(message)
		}
	}
	// Removing Substrate from under a kagent installed elsewhere strands its
	// WorkerPool, so a mismatched --namespace stops before anything is deleted.
	releaseNamespaces, err := c.kagentReleaseNamespaces(ctx)
	if err != nil {
		return result, err
	}
	if len(releaseNamespaces) > 0 && !slices.Contains(releaseNamespaces, namespace) {
		return result, fmt.Errorf("kagent is installed in namespace %s, not %s; rerun with --namespace %s", strings.Join(releaseNamespaces, ", "), namespace, releaseNamespaces[0])
	}

	release := func(name, releaseNamespace string) error {
		report("Uninstalling " + name)
		found, err := c.uninstallChart(ctx, name, releaseNamespace)
		if err == nil && !found {
			result.MissingReleases = append(result.MissingReleases, name)
		}
		return err
	}

	// The kagent controller finalizes SandboxTemplates, so they go while it runs.
	// Otherwise deleting their CRD waits forever.
	report(" Deleting sandbox templates")
	if err := c.deleteServed(ctx, "sandboxtemplates.api.kagent.dev", "--all", "--all-namespaces"); err != nil {
		return result, fmt.Errorf("delete sandbox templates: %w", err)
	}
	// kagent goes before Substrate. Its release holds the Substrate WorkerPool,
	// and Helm can delete that only while substrate-crds still serves the kind.
	if err := release("kagent", namespace); err != nil {
		return result, err
	}
	// atecontroller releases a SandboxConfig once no WorkerPool uses it. With the
	// WorkerPool gone, the configs go while atecontroller still runs.
	report(" Deleting Substrate sandbox configs")
	if err := c.deleteServed(ctx, "sandboxconfigs.ate.dev", "--all"); err != nil {
		return result, fmt.Errorf("delete Substrate sandbox configs: %w", err)
	}
	if err := release("substrate", substrateNamespace); err != nil {
		return result, err
	}
	bundled, err := c.bundledPostgresExists(ctx, namespace)
	if err != nil {
		return result, err
	}
	if bundled {
		report(" Deleting bundled PostgreSQL")
		if err := c.deleteBundledPostgres(ctx, namespace, postgresImage); err != nil {
			return result, err
		}
	}
	if err := release(podCertificateRelease, podCertificateNamespace); err != nil {
		return result, err
	}
	// The CRDs go only through their releases. A list of CRD names falls behind
	// the chart, so CRDs applied outside Helm stay.
	if err := release("substrate-crds", substrateNamespace); err != nil {
		return result, err
	}
	if err := release("kagent-crds", namespace); err != nil {
		return result, err
	}

	// Install labels the kagent namespace so Substrate's API admits it. The
	// namespace stays, so only the label goes. A null merge patch is a no-op when
	// the label is absent, so the only "not found" is a namespace that is gone.
	report(" Removing the Substrate API access label")
	if out, err := c.kubectl(ctx, "", "patch", "namespace", namespace, "--type=merge",
		"-p", `{"metadata":{"labels":{"ate.dev/api-access":null}}}`); err != nil && !strings.Contains(out+err.Error(), "not found") {
		return result, fmt.Errorf("remove the Substrate API access label from namespace %s: %w", namespace, err)
	}

	// The Substrate prerequisites live outside any release, in these namespaces.
	// They go after substrate-crds, whose release record ate-system holds. The
	// kagent namespace stays. Helm creates it without recording ownership, and
	// --namespace can name a namespace that other workloads share.
	namespaces := []string{podCertificateNamespace, substrateNamespace}
	report(" Deleting Substrate namespaces")
	if _, err := c.kubectl(ctx, "", append(append([]string{"delete", "namespace"}, namespaces...), "--ignore-not-found")...); err != nil {
		return result, fmt.Errorf("delete Substrate namespaces: %w", err)
	}
	// The controllers republish their trust bundles, so the bundles go once the
	// namespace deletes above have waited the controllers out.
	report(" Deleting Substrate trust bundles")
	if err := c.deleteServed(ctx, "clustertrustbundles.certificates.k8s.io", substrateTrustBundles...); err != nil {
		return result, fmt.Errorf("delete Substrate trust bundles: %w", err)
	}

	return result, nil
}
