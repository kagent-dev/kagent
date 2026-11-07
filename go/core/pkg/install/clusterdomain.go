package install

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	coreDNSNamespace = "kube-system"
	coreDNSConfigMap = "coredns"
	coreDNSCorefile  = "Corefile"
)

// corefileClusterDomain returns the first zone served by the CoreDNS kubernetes
// plugin, such as cluster.local in "kubernetes cluster.local in-addr.arpa ip6.arpa {".
// Reverse-lookup zones are skipped. It returns "" when no usable zone is present.
func corefileClusterDomain(corefile string) string {
	scanner := bufio.NewScanner(strings.NewReader(corefile))
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "#")
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "kubernetes" {
			continue
		}
		for _, zone := range fields[1:] {
			if zone == "{" {
				break
			}
			zone = strings.TrimSuffix(zone, ".")
			if strings.HasSuffix(zone, ".arpa") || len(validation.IsDNS1123Subdomain(zone)) != 0 {
				continue
			}
			return zone
		}
	}
	return ""
}

// DetectClusterDomain reads the cluster DNS domain from the CoreDNS
// configuration for Config.ClusterDomain. Clusters without a CoreDNS ConfigMap,
// or whose Corefile names no zone, return "" so the chart keeps its
// cluster.local default.
func (c *Client) DetectClusterDomain(ctx context.Context) (string, error) {
	configMap, err := c.kubernetes.CoreV1().ConfigMaps(coreDNSNamespace).Get(ctx, coreDNSConfigMap, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to read ConfigMap %s/%s to detect the cluster domain: %w", coreDNSNamespace, coreDNSConfigMap, err)
	}
	return corefileClusterDomain(configMap.Data[coreDNSCorefile]), nil
}
