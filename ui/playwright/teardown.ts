// Playwright globalTeardown — stop the controller port-forward started in setup.ts.

import { existsSync, readFileSync, unlinkSync } from "node:fs";
import * as path from "node:path";

const PID_FILE = path.join(__dirname, ".e2e-pids.json");

export default async function globalTeardown() {
  if (!existsSync(PID_FILE)) return;
  try {
    const { portForward } = JSON.parse(readFileSync(PID_FILE, "utf-8"));
    if (portForward) {
      console.log(`\n=== E2E: stopping port-forward (pid ${portForward}) ===`);
      try {
        // Negative pid kills the detached process group (kubectl + children).
        process.kill(-portForward, "SIGTERM");
      } catch {
        try {
          process.kill(portForward, "SIGTERM");
        } catch {
          // Already gone.
        }
      }
    }
    unlinkSync(PID_FILE);
  } catch (err) {
    console.warn("Warning cleaning up port-forward:", err);
  }
}
