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

const READY_TIMEOUT_MS = 60_000;
const PROBE_INTERVAL_MS = 2_000;
const PROBE_TIMEOUT_MS = 5_000;
// Require several consecutive good probes before declaring the backend ready. A
// `kubectl port-forward` tunnel can accept one connection and then drop the next
// while it is still stabilizing, so a single success is not enough to start tests.
const REQUIRED_OK_PROBES = 3;

// Resolve once the controller answers with a non-5xx status (401/404 count — it's
// serving) on REQUIRED_OK_PROBES consecutive probes. A 5xx or a connection failure
// resets the streak and keeps waiting. Reject immediately if the port-forward has
// already died (isPortForwardDead returns its exit reason), so a dead tunnel or a
// missing kubectl fails loudly in seconds instead of a vague 60s timeout.
function waitForBackend(
  url: string,
  timeoutMs: number,
  isPortForwardDead: () => string | null,
): Promise<void> {
  const start = Date.now();
  let consecutive = 0;
  return new Promise((resolve, reject) => {
    const retryOrTimeout = () => {
      consecutive = 0;
      if (Date.now() - start > timeoutMs) {
        reject(new Error(`Timed out waiting for backend at ${url} after ${timeoutMs}ms`));
      } else {
        setTimeout(check, PROBE_INTERVAL_MS);
      }
    };
    const check = () => {
      const dead = isPortForwardDead();
      if (dead) {
        reject(new Error(`port-forward exited before the backend became reachable (${dead})`));
        return;
      }
      fetch(url, { signal: AbortSignal.timeout(PROBE_TIMEOUT_MS) })
        .then((res) => {
          if (res.status >= 500) {
            retryOrTimeout();
            return;
          }
          consecutive += 1;
          if (consecutive >= REQUIRED_OK_PROBES) {
            resolve();
          } else {
            setTimeout(check, PROBE_INTERVAL_MS);
          }
        })
        .catch(retryOrTimeout);
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

  // Track early death so waitForBackend can fail fast with the reason instead of
  // spending the full timeout probing a tunnel that will never come up (e.g. kubectl
  // missing, wrong context, or the controller not deployed).
  let pfExit: string | null = null;
  pf.on("error", (err) => {
    pfExit = `spawn error: ${err.message}`;
  });
  pf.on("exit", (code, signal) => {
    pfExit = `exited code=${code ?? "null"} signal=${signal ?? "null"}`;
  });

  // Persist the pid so globalTeardown can stop it (write even before waiting so a
  // failed wait still leaves a killable pid).
  writeFileSync(PID_FILE, JSON.stringify({ portForward: pf.pid }));

  await waitForBackend(`http://127.0.0.1:${LOCAL_PORT}/api/agents`, READY_TIMEOUT_MS, () => pfExit);
  console.log("kagent-controller reachable — starting tests.\n");
}
