// Playwright globalSetup — bridge the local test run to the REAL kagent backend
// running in the kind cluster.
//
// The cluster itself is created by playwright/scripts/setup.sh (run it once before
// `yarn run e2e`; CI runs it as a workflow step). This setup only port-forwards the
// controller so the proxy (mocks/server.mjs, pointed at KAGENT_BACKEND_URL) can
// reach it. The chat A2A stream is the only thing the proxy mocks; every other
// /api call goes to this real backend.

import { spawn } from "node:child_process";
import { writeFileSync } from "node:fs";
import * as path from "node:path";
import { CONTROLLER_PORT, LOCAL_PORT } from "./backend";

const PID_FILE = path.join(__dirname, ".e2e-pids.json");
const KUBE_CONTEXT = process.env.KUBE_CONTEXT || "kind-kagent";
const NAMESPACE = process.env.KUBE_NAMESPACE || "kagent";
const CONTROLLER_SERVICE = "kagent-controller";

// Any HTTP response (even 401/404) means the controller is serving; only a
// connection failure means the port-forward isn't ready yet.
function waitForBackend(url: string, timeoutMs: number): Promise<void> {
  const start = Date.now();
  return new Promise((resolve, reject) => {
    const check = () => {
      fetch(url, { signal: AbortSignal.timeout(5_000) })
        .then(() => resolve())
        .catch(() => {
          if (Date.now() - start > timeoutMs) {
            reject(new Error(`Timed out waiting for backend at ${url} after ${timeoutMs}ms`));
          } else {
            setTimeout(check, 2_000);
          }
        });
    };
    check();
  });
}

export default async function globalSetup() {
  console.log("\n=== E2E: port-forward kagent-controller ===");
  console.log(`context=${KUBE_CONTEXT} ns=${NAMESPACE} ${LOCAL_PORT} -> ${CONTROLLER_SERVICE}:${CONTROLLER_PORT}`);

  const pf = spawn(
    "kubectl",
    [
      "port-forward",
      `service/${CONTROLLER_SERVICE}`,
      "-n",
      NAMESPACE,
      "--context",
      KUBE_CONTEXT,
      `${LOCAL_PORT}:${CONTROLLER_PORT}`,
    ],
    { stdio: "pipe", detached: true },
  );
  pf.unref();
  pf.stdout?.on("data", (d: Buffer) => process.stdout.write(`[port-forward] ${d}`));
  pf.stderr?.on("data", (d: Buffer) => process.stderr.write(`[port-forward] ${d}`));

  // Persist the pid so globalTeardown can stop it (write even before waiting so a
  // failed wait still leaves a killable pid).
  writeFileSync(PID_FILE, JSON.stringify({ portForward: pf.pid }));

  await waitForBackend(`http://127.0.0.1:${LOCAL_PORT}/api/agents`, 60_000);
  console.log("kagent-controller reachable — starting tests.\n");
}
