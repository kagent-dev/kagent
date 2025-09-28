const debugMode = process.env.RUNNER_DEBUG === '1' || process.env.DEBUG_MODE === 'true';

function debugLog(...args) {
  if (debugMode && args.length > 0) {
    console.log(...args);
  }
}

module.exports = { debugLog };