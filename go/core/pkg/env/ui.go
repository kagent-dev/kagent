package env

// The controller's UI listener and Vite map the same deployment names to browser keys.
var (
	UIDir              = RegisterStringVar("KAGENT_UI_DIR", "/usr/share/kagent/ui", "Directory of the built UI bundle the controller serves. Without an index.html there, the UI listener is off.", ComponentController)
	UIBindAddress      = RegisterStringVar("KAGENT_UI_BIND_ADDRESS", ":8080", "Address of the controller's UI listener, which serves the bundle and the browser-facing API paths.", ComponentController)
	UIForwardedHeaders = RegisterStringVar("KAGENT_UI_FORWARDED_HEADERS", "", "Comma-separated auth-proxy identity headers the UI listener keeps; the rest are stripped from browser requests.", ComponentController)

	UIAPIBaseURL      = RegisterStringVar("KAGENT_UI_API_BASE_URL", "/api", "Browser API base URL for UI containers and Vite development.", ComponentUI)
	UIStreamTimeoutMS = RegisterStringVar("KAGENT_UI_STREAM_TIMEOUT_MS", "1800000", "UI chat stream inactivity timeout in milliseconds. 0 disables the timeout. Applies in containers and Vite development.", ComponentUI)
	UIBasePath        = RegisterStringVar("KAGENT_UI_BASE_PATH", "", "UI public path prefix, such as /ui; empty serves at the root. Applies in containers and Vite development; the container falls back to the root for invalid or reserved prefixes.", ComponentUI)
	UISSORedirectPath = RegisterStringVar("KAGENT_UI_SSO_REDIRECT_PATH", "/oauth2/start", "UI path used by Sign in with SSO.", ComponentUI)
	UIEnableMock      = RegisterBoolVar("KAGENT_UI_ENABLE_MOCK", false, "Serve the development UI from in-browser fixtures when true. Overrides backend settings; no user is signed in. Release bundles do not include the mock backend.", ComponentUI)
	_                 = RegisterStringVar("KAGENT_UI_EXTENSION_<NAME>", "", "UI extension settings forwarded to the browser at runtime. Each installed extension owns its keys and defaults; these values are public.", ComponentUI)
	_                 = RegisterStringVar("KAGENT_UI_DEV_CONTROLLER_URL", "http://127.0.0.1:8083", "Vite development proxy target for /api and /a2a; not sent to the browser.", ComponentUI)
	_                 = RegisterBoolVar("KAGENT_UI_VITE_EXAMPLE_EXTENSION", false, "Build-time switch enabling the bundled example UI extension.", ComponentUI)
	_                 = RegisterStringVar("KAGENT_UI_VITE_API_MODE", "", "Build-time API mode override, mock or live, used by UI tests. Overrides KAGENT_UI_ENABLE_MOCK; leave unset for normal development.", ComponentUI, ComponentTesting)
)
