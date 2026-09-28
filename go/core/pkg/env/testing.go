package env

// Testing environment variables used in E2E tests and mock services.
var (
	KagentLocalHost = RegisterStringVar(
		"KAGENT_E2E_LOCAL_HOST",
		"",
		"Host reachable from E2E runtimes for local mock servers. Defaults to 172.17.0.1 on Linux and host.docker.internal on macOS; required on other systems.",
		ComponentTesting,
	)

	UpdateGolden = RegisterBoolVar(
		"KAGENT_TEST_UPDATE_GOLDEN",
		false,
		"When true, update golden test files instead of comparing.",
		ComponentTesting,
	)

	STSPort = RegisterStringVar(
		"KAGENT_TEST_STS_PORT",
		"8091",
		"Port for the mock STS (Security Token Service) server.",
		ComponentTesting,
	)

	LLMPort = RegisterStringVar(
		"KAGENT_TEST_LLM_PORT",
		"8090",
		"Port for the mock LLM server.",
		ComponentTesting,
	)
)

var (
	TestPython                = RegisterStringVar("KAGENT_TEST_PYTHON", "", "Python executable enabling the Python TaskStore/PostgreSQL interoperability test. That subtest is not run when unset.", ComponentTesting)
	E2EAPIURL                 = RegisterStringVar("KAGENT_E2E_API_URL", "", "Controller URL for Go E2E tests. Falls back to KAGENT_API_URL; tests skip when neither is set.", ComponentTesting)
	E2ECLI                    = RegisterStringVar("KAGENT_E2E_CLI", "", "CLI executable for catalog lifecycle E2E tests; make -C go e2e builds and supplies it.", ComponentTesting)
	E2ERuntimeImage           = RegisterStringVar("KAGENT_E2E_RUNTIME_IMAGE", "", "Required digest-pinned Go ADK image for sandbox E2E tests and lifecycle manifests.", ComponentTesting)
	_                         = RegisterStringVar("KAGENT_E2E_BYO_IMAGE", "", "Digest-pinned BYO image substituted into E2E lifecycle manifests.", ComponentTesting)
	_                         = RegisterStringVar("KAGENT_E2E_CLAUDE_IMAGE", "", "Digest-pinned Claude harness image substituted into E2E lifecycle manifests.", ComponentTesting)
	_                         = RegisterStringVar("KAGENT_E2E_CODEX_IMAGE", "", "Digest-pinned Codex harness image substituted into E2E lifecycle manifests.", ComponentTesting)
	E2ESandboxNamespace       = RegisterStringVar("KAGENT_E2E_SANDBOX_NAMESPACE", "kagent", "Namespace for sandbox E2E resources.", ComponentTesting)
	E2ESandboxWorkerPool      = RegisterStringVar("KAGENT_E2E_SANDBOX_WORKER_POOL", "kagent-default", "Worker pool used by sandbox E2E resources.", ComponentTesting)
	E2EOTLPListenAddress      = RegisterStringVar("KAGENT_E2E_OTLP_LISTEN_ADDRESS", "", "Listen address for the E2E suite's OTLP trace receiver. Unset disables the shared receiver.", ComponentTesting)
	E2ERequireTracing         = RegisterStringVar("KAGENT_E2E_REQUIRE_TRACING", "false", "Fail instead of skip when expected native harness tracing support is unavailable. Enabled by true, ignoring case and surrounding whitespace.", ComponentTesting)
	E2ERunUpgradeTests        = RegisterStringVar("KAGENT_E2E_RUN_UPGRADE_TESTS", "false", "Run upgrade integration tests when exactly true.", ComponentTesting)
	E2ERunRollingUpgradeTests = RegisterStringVar("KAGENT_E2E_RUN_ROLLING_UPGRADE_TESTS", "false", "Run rolling upgrade integration tests when exactly true.", ComponentTesting)
	_                         = RegisterStringVar("KAGENT_E2E_UI_LOOP_PORT", "8001", "Vite development/preview port and UI browser-test port.", ComponentTesting)
	_                         = RegisterStringVar("KAGENT_E2E_UI_LOOP_EXTENSION_PORT", "", "Example-extension browser-test port. Defaults to KAGENT_E2E_UI_LOOP_PORT plus 50.", ComponentTesting)
	_                         = RegisterBoolVar("KAGENT_E2E_UI_LOOP_LIVE", false, "Run UI browser tests against a real cluster when exactly true.", ComponentTesting)
	_                         = RegisterStringVar("KAGENT_E2E_UI_LOOP_LIVE_PORT", "8301", "Live-cluster UI browser-test port.", ComponentTesting)
)

// Upgrade test inputs supplied by the root Make targets. Their Make variable
// names remain shared with build tooling; the Go tests consume these scoped names.
var (
	E2ERepoRoot           = RegisterStringVar("KAGENT_E2E_REPO_ROOT", "", "Repository root for upgrade tests. Falls back to git rev-parse --show-toplevel.", ComponentTesting)
	E2EUpgradeFromVersion = RegisterStringVar("KAGENT_E2E_UPGRADE_FROM_VERSION", "", "Required previous release version for upgrade tests; supplied by make from UPGRADE_FROM_VERSION.", ComponentTesting)
	E2EVersion            = RegisterStringVar("KAGENT_E2E_VERSION", "", "Required current image tag for upgrade tests; supplied by make from VERSION.", ComponentTesting)
	E2EDockerRegistry     = RegisterStringVar("KAGENT_E2E_DOCKER_REGISTRY", "localhost:5001", "Image registry for upgrade tests; supplied by make from DOCKER_REGISTRY.", ComponentTesting)
	E2EKindClusterName    = RegisterStringVar("KAGENT_E2E_KIND_CLUSTER_NAME", "kagent", "Kind cluster used by upgrade tests; supplied by make from KIND_CLUSTER_NAME.", ComponentTesting)
	E2ENamespace          = RegisterStringVar("KAGENT_E2E_NAMESPACE", "kagent", "Kubernetes namespace used by upgrade tests.", ComponentTesting)
	E2EKubeContext        = RegisterStringVar("KAGENT_E2E_KUBE_CONTEXT", "", "Kubernetes context for upgrade tests. Defaults to kind- followed by KAGENT_E2E_KIND_CLUSTER_NAME.", ComponentTesting)
)
