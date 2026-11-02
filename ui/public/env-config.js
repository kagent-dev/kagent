// Placeholder. Never ships values.
//
// The controller serves its own copy, rendered from its environment on every start
// (see `go/core/internal/uiserver`), and the dev server replaces the script tag that loads
// it with an inline copy. An empty object here means a build that somehow served
// this file unmodified falls back to the app's defaults rather than to whatever
// the machine that built the image happened to have set.
window.environmentVariables = {};
