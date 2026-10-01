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
