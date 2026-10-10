package env

var BuiltinHarnessImages = RegisterStringVar("KAGENT_BUILTIN_HARNESS_IMAGES", "{}", "JSON object containing release, kagent, claude, and codex default image references. Images must be pinned by sha256 digest. A nonempty release must match the controller version.", ComponentController)
