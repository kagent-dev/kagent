// Package skillsinit fetches skill and plugin artifacts from remote sources.
package skillsinit

// S3Ref describes a single S3 fetch into Dest.
type S3Ref struct {
	URI       string `json:"uri"`
	Dest      string `json:"dest"`
	Endpoint  string `json:"endpoint,omitempty"`
	Region    string `json:"region,omitempty"`
	VersionID string `json:"versionId,omitempty"`
}

// OCIRef describes a single OCI image to pull and extract.
type OCIRef struct {
	Image string `json:"image"`
	Dest  string `json:"dest"`
}
