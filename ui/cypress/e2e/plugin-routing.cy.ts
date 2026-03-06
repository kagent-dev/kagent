/**
 * Plugin routing E2E tests.
 *
 * Uses cy.intercept() to mock /api/plugins and /_p/ responses so the tests
 * run without a real Go backend or plugin service.
 */

const MOCK_PLUGIN_HTML = `
<!DOCTYPE html>
<html><head><title>Mock Plugin</title></head>
<body>
  <div data-testid="mock-plugin-content">Mock Plugin Loaded</div>
  <script>
    window.addEventListener("message", function(e) {
      if (e.data && e.data.type === "kagent:context") {
        document.body.setAttribute("data-theme", e.data.payload.theme || "");
        document.body.setAttribute("data-context-received", "true");
      }
    });
    window.parent.postMessage({ type: "kagent:ready", payload: {} }, "*");
  </script>
</body>
</html>`;

function setupPluginMocks() {
  cy.intercept("GET", "/api/plugins", {
    statusCode: 200,
    body: {
      data: [
        {
          name: "default/test-plugin",
          pathPrefix: "test-plugin",
          displayName: "Test Plugin",
          icon: "puzzle",
          section: "AGENTS",
        },
      ],
    },
  }).as("getPlugins");

  cy.intercept("GET", "/_p/test-plugin/**", {
    statusCode: 200,
    headers: { "content-type": "text/html" },
    body: MOCK_PLUGIN_HTML,
  }).as("getPluginProxy");
}

function skipOnboarding() {
  cy.visit("/", {
    onBeforeLoad(win) {
      win.localStorage.setItem("kagent-onboarding", "true");
    },
  });
}

describe("Plugin Routing", () => {
  describe("Sidebar plugin items", () => {
    it("shows plugin nav item from /api/plugins in the correct section", () => {
      setupPluginMocks();
      skipOnboarding();

      cy.wait("@getPlugins");

      // Plugin should appear in the AGENTS section
      cy.contains("span", "Test Plugin").should("be.visible");
      // The link should point to /plugins/test-plugin
      cy.contains("a", "Test Plugin").should(
        "have.attr",
        "href",
        "/plugins/test-plugin"
      );
    });

    it("clicking plugin nav item navigates to /plugins/{name}", () => {
      setupPluginMocks();
      skipOnboarding();

      cy.wait("@getPlugins");

      cy.contains("a", "Test Plugin").click();
      cy.url().should("include", "/plugins/test-plugin");

      // Page should contain the iframe shell
      cy.get('iframe[title="Plugin: test-plugin"]').should("exist");
      cy.get('iframe[title="Plugin: test-plugin"]')
        .should("have.attr", "src")
        .and("include", "/_p/test-plugin");
    });
  });

  describe("Plugin page", () => {
    it("hard refresh on /plugins/{name} preserves sidebar and iframe", () => {
      setupPluginMocks();
      cy.visit("/plugins/test-plugin", {
        onBeforeLoad(win) {
          win.localStorage.setItem("kagent-onboarding", "true");
        },
      });
      cy.wait("@getPlugins");

      // Sidebar still visible with plugin item
      cy.contains("span", "Test Plugin").should("be.visible");
      // Iframe present
      cy.get('iframe[title="Plugin: test-plugin"]').should("exist");
    });

    it("sends kagent:context to iframe via postMessage", () => {
      setupPluginMocks();
      cy.visit("/plugins/test-plugin", {
        onBeforeLoad(win) {
          win.localStorage.setItem("kagent-onboarding", "true");
        },
      });
      cy.wait("@getPluginProxy");

      // Wait for iframe to load and receive context
      cy.get('iframe[title="Plugin: test-plugin"]')
        .its("0.contentDocument.body", { timeout: 10000 })
        .should("have.attr", "data-context-received", "true");
    });

    it("shows loading state before iframe loads", () => {
      // Delay the proxy response to observe loading state
      cy.intercept("GET", "/api/plugins", {
        statusCode: 200,
        body: { data: [] },
      });
      cy.intercept("GET", "/_p/test-plugin/**", {
        statusCode: 200,
        headers: { "content-type": "text/html" },
        body: MOCK_PLUGIN_HTML,
        delay: 2000,
      });
      cy.visit("/plugins/test-plugin", {
        onBeforeLoad(win) {
          win.localStorage.setItem("kagent-onboarding", "true");
        },
      });
      cy.get('[data-testid="plugin-loading"]').should("be.visible");
    });
  });

  describe("Badge updates", () => {
    it("badge appears in sidebar when plugin sends kagent:badge", () => {
      setupPluginMocks();
      cy.visit("/plugins/test-plugin", {
        onBeforeLoad(win) {
          win.localStorage.setItem("kagent-onboarding", "true");
        },
      });
      cy.wait("@getPlugins");
      cy.wait("@getPluginProxy");

      // Dispatch a badge event from the iframe
      cy.get('iframe[title="Plugin: test-plugin"]')
        .its("0.contentWindow", { timeout: 10000 })
        .then((iframeWin) => {
          // Plugin sends badge message to parent
          iframeWin.parent.postMessage(
            {
              type: "kagent:badge",
              payload: { count: 3 },
            },
            "*"
          );
        });

      // Badge should appear next to the plugin nav item
      // The SidebarMenuBadge renders the count
      cy.contains("a", "Test Plugin")
        .closest("li")
        .find('[data-sidebar="menu-badge"]')
        .should("contain.text", "3");
    });
  });

  describe("Error handling", () => {
    it("shows error state with retry when /api/plugins fails", () => {
      cy.intercept("GET", "/api/plugins", {
        statusCode: 500,
        body: "Internal Server Error",
      }).as("getPluginsFail");
      cy.visit("/", {
        onBeforeLoad(win) {
          win.localStorage.setItem("kagent-onboarding", "true");
        },
      });
      cy.wait("@getPluginsFail");

      cy.get('[data-testid="plugins-error"]').should("be.visible");
      cy.contains("Plugins failed").should("be.visible");
      cy.get('[data-testid="plugins-retry"]').should("be.visible");

      // Setup successful response for retry
      cy.intercept("GET", "/api/plugins", {
        statusCode: 200,
        body: {
          data: [
            {
              name: "default/test-plugin",
              pathPrefix: "test-plugin",
              displayName: "Test Plugin",
              icon: "puzzle",
              section: "AGENTS",
            },
          ],
        },
      }).as("getPluginsRetry");

      cy.get('[data-testid="plugins-retry"]').click();
      cy.wait("@getPluginsRetry");

      cy.get('[data-testid="plugins-error"]').should("not.exist");
      cy.contains("span", "Test Plugin").should("be.visible");
    });

    it("keeps plugin shell mounted when upstream is unreachable", () => {
      cy.intercept("GET", "/api/plugins", {
        statusCode: 200,
        body: { data: [] },
      });
      cy.intercept("GET", "/_p/unreachable-plugin/**", {
        forceNetworkError: true,
      }).as("pluginNetError");

      cy.visit("/plugins/unreachable-plugin", {
        onBeforeLoad(win) {
          win.localStorage.setItem("kagent-onboarding", "true");
        },
      });

      cy.get('iframe[title="Plugin: unreachable-plugin"]', { timeout: 10000 })
        .should("have.attr", "src")
        .and("include", "/_p/unreachable-plugin");
      cy.get('[data-testid="plugin-loading"]').should("be.visible");
    });
  });
});
