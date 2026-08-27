import React from "react";
import ReactDOM from "react-dom/client";
import { isMockMode } from "./api/config";
import { activeVendorExtensionConfig } from "./vendorExtensions/activeConfig";
import { loadVendorStylesheets } from "./vendorExtensions/theme";
import { applyVendorDocumentTitle } from "./vendorExtensions/branding";
import { AuthProvider } from "./auth";
import { App } from "./App";

async function bootstrap() {
  // Deployment configuration needs no step here: it arrives on `window` from a
  // script tag ahead of this module, so it is already readable — including by the
  // module-level constants in `api/config.ts` that resolved as this file's
  // imports were evaluated.

  // Before the first render: a web font that arrives afterwards reflows
  // everything already painted.
  loadVendorStylesheets(activeVendorExtensionConfig.theme);
  applyVendorDocumentTitle(activeVendorExtensionConfig.branding);

  // Which backend is serving is decided in one place, `api/config.ts`, and read
  // here rather than re-derived: two independent readings of the same env var
  // can disagree, and the failure mode is a production build quietly answering
  // from fixtures.
  if (isMockMode) {
    const { startMockBackend } = await import("./mocks/startMockBackend");
    await startMockBackend();
  }

  ReactDOM.createRoot(document.getElementById("root")!).render(
    <React.StrictMode>
      {/* Outside <App> because authentication is not a vendor concern: the
          extension config's provider list belongs to whoever installs an
          extension, and core auth must exist whether or not one is present. */}
      <AuthProvider>
        <App />
      </AuthProvider>
    </React.StrictMode>,
  );
}

void bootstrap();
