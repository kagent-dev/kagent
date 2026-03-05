// kagent-plugin-bridge.js — lightweight bridge for plugin UIs
// Include this script in your plugin's HTML to communicate with the kagent host.
//
// Usage:
//   <script src="kagent-plugin-bridge.js"></script>
//   <script>
//     kagent.onContext(({ theme, namespace }) => {
//       document.documentElement.classList.toggle('dark', theme === 'dark');
//     });
//     kagent.connect();
//   </script>
//
// Protocol: all messages use { type: "kagent:<action>", payload: {...} }
// Host -> Plugin: kagent:context (theme, namespace, authToken)
// Plugin -> Host: kagent:ready, kagent:navigate, kagent:resize, kagent:badge, kagent:title

const kagent = {
  _ready: false,
  _listeners: {},

  // Call on plugin load to establish connection with kagent host
  connect() {
    window.addEventListener("message", (event) => {
      if (event.data?.type === "kagent:context") {
        const { theme, namespace, authToken } = event.data.payload;
        this._emit("context", { theme, namespace, authToken });
      }
    });
    window.parent.postMessage({ type: "kagent:ready", payload: {} }, "*");
    this._ready = true;
  },

  // Listen for context updates (theme, namespace, auth changes)
  onContext(fn) {
    this._on("context", fn);
  },

  // Request host navigation to a different page
  navigate(href) {
    window.parent.postMessage({ type: "kagent:navigate", payload: { href } }, "*");
  },

  // Update sidebar badge for this plugin
  setBadge(count, label) {
    window.parent.postMessage({ type: "kagent:badge", payload: { count, label } }, "*");
  },

  // Set page title shown above the iframe
  setTitle(title) {
    window.parent.postMessage({ type: "kagent:title", payload: { title } }, "*");
  },

  // Report content height for auto-resize (defaults to document.body.scrollHeight)
  reportHeight(height) {
    window.parent.postMessage(
      { type: "kagent:resize", payload: { height: height ?? document.body.scrollHeight } },
      "*"
    );
  },

  _on(event, fn) {
    (this._listeners[event] ??= []).push(fn);
  },
  _emit(event, data) {
    (this._listeners[event] ?? []).forEach((fn) => fn(data));
  },
};
