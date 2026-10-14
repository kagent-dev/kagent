package trivy

import rego.v1

default ignore := false

# The controller uses Istio's Kubernetes client and KRT libraries, not istiod
# or Envoy. Trivy compares the v0.0.0 pseudo-version of this 2026 commit against
# the 2019-2022 release versions in these mesh server/proxy advisories.
# Keep this exception tied to the reviewed module version.
ignore if {
	input.PkgName == "istio.io/istio"
	input.InstalledVersion == "v0.0.0-20260820015531-47320ba1a73b"
	input.VulnerabilityID in {
		"CVE-2019-14993", # Envoy regex handling: https://github.com/advisories/GHSA-qcvw-82hh-gq38
		"CVE-2021-39155", # Host authorization: https://github.com/advisories/GHSA-7774-7vr3-cc8j
		"CVE-2021-39156", # URI authorization: https://github.com/advisories/GHSA-hqxw-mm44-gc4r
		"CVE-2022-23635", # istiod port 15012: https://github.com/advisories/GHSA-856q-xv3c-7f2f
	}
}

# The GitHub CLI the Harness images install from its release (go/github-cli.version):
# gh v2.102.0, the newest release, is built with Go 1.27.1 and golang.org/x/net
# v0.58.0; cli/cli's main already moved to Go 1.27.2 and x/net v0.60.0, which the
# next release ships. Until then these findings are accepted at those versions, which
# only gh carries (the line builds its own binaries with Go 1.27.2 and x/net v0.60.0;
# Trivy hands the policy no binary path), and only up to the expiry: a release
# Renovate brings clears them, and an expired exception fails the scan again
# (giantswarm/kagent-upstream#575).
ignore if {
	time.now_ns() < time.parse_rfc3339_ns("2026-11-14T00:00:00Z")
	gh_release_findings[input.PkgName][input.InstalledVersion][_] == input.VulnerabilityID
}

gh_release_findings := {
	"stdlib": {"v1.27.1": ["CVE-2026-78667", "CVE-2026-78669", "CVE-2026-97031"]},
	"golang.org/x/net": {"v0.58.0": ["CVE-2026-78669"]},
}
