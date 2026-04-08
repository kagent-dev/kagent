package env

// Testing environment variables used in E2E tests and mock services.
var (
	KagentLocalHost = RegisterStringVar(
		"KAGENT_LOCAL_HOST",
		"",
		"Local host override for E2E tests.",
		ComponentTesting,
	)

	SkipCleanup = RegisterBoolVar(
		"SKIP_CLEANUP",
		false,
		"When true, skip cleanup after E2E tests for debugging.",
		ComponentTesting,
	)

	UpdateGolden = RegisterBoolVar(
		"UPDATE_GOLDEN",
		false,
		"When true, update golden test files instead of comparing.",
		ComponentTesting,
	)

	STSPort = RegisterStringVar(
		"STS_PORT",
		"",
		"Port for the mock STS (Security Token Service) server.",
		ComponentTesting,
	)

	LLMPort = RegisterStringVar(
		"LLM_PORT",
		"",
		"Port for the mock LLM server.",
		ComponentTesting,
	)
)
