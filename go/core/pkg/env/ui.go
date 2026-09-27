package env

// The container entrypoint maps KAGENT_* deployment names to browser keys.
// Vite reads the browser key names directly from the development environment.
var (
	_ = RegisterStringVar("KAGENT_API_BASE_URL", "/api", "UI container setting mapped to the browser's API_BASE_URL at startup.", ComponentUI)
	_ = RegisterStringVar("KAGENT_STREAM_TIMEOUT_MS", "1800000", "UI container chat stream inactivity timeout in milliseconds, mapped to STREAM_TIMEOUT_MS. 0 disables the timeout.", ComponentUI)
	_ = RegisterStringVar("KAGENT_UI_BASE_PATH", "", "UI container public path prefix, such as /ui; empty serves at the root. Mapped to BASE_PATH; invalid or reserved prefixes fall back to the root.", ComponentUI)
	_ = RegisterStringVar("API_BASE_URL", "/api", "Browser API base URL. Set directly for Vite development; use KAGENT_API_BASE_URL in the UI container.", ComponentUI)
	_ = RegisterStringVar("STREAM_TIMEOUT_MS", "1800000", "Browser chat stream inactivity timeout in milliseconds. Set directly for Vite development; use KAGENT_STREAM_TIMEOUT_MS in the UI container. 0 disables the timeout.", ComponentUI)
	_ = RegisterStringVar("BASE_PATH", "", "Browser public path prefix. Set directly for Vite development; use KAGENT_UI_BASE_PATH in the UI container.", ComponentUI)
	_ = RegisterStringVar("SSO_REDIRECT_PATH", "/oauth2/start", "UI path used by Sign in with SSO.", ComponentUI)
	_ = RegisterBoolVar("ENABLE_MOCK_UI", false, "Serve the UI from in-browser fixtures when true. Overrides backend settings; no user is signed in. Applies in development and containers.", ComponentUI)
	_ = RegisterStringVar("EXTENSION_<NAME>", "", "UI extension settings forwarded to the browser at runtime. Each installed extension owns its keys and defaults; these values are public.", ComponentUI)
	_ = RegisterStringVar("KAGENT_DEV_CONTROLLER_URL", "http://127.0.0.1:8083", "Vite development proxy target for /api and /a2a; not sent to the browser.", ComponentUI)
	_ = RegisterBoolVar("VITE_EXAMPLE_EXTENSION", false, "Build-time switch enabling the bundled example UI extension.", ComponentUI)
)
