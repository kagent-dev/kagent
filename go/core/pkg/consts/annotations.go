// Package consts holds well-known constant keys (annotations, labels) shared across kagent's
// core packages. They are defined once here so producers and consumers in different packages
// cannot drift out of sync.
package consts

// ConfigHashAnnotation is the annotation key carrying the hash of an agent's rendered config,
// used to propagate config changes. On the Deployment path the translator stamps it on the agent
// pod template so a config change rolls the Deployment; on the substrate path it is mirrored onto
// the generated ActorTemplate to drive a new golden snapshot and session actor. It is shared here
// because the writer (translator) and the substrate backend (writer/reader) live in different
// packages and must agree on the key.
const ConfigHashAnnotation = "kagent.dev/config-hash"
