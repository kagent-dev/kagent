#!/bin/sh
# Ensure the persistent Hermes messaging gateway (Slack/Telegram/...) is running.
#
# This is a long-lived process distinct from the per-connection `hermes acp`
# child: it polls/streams the configured platforms. Channels are auto-detected
# from the unsuffixed env contract (SLACK_BOT_TOKEN / TELEGRAM_BOT_TOKEN / ...).
# Under Substrate the actor is checkpointed/restored, so the gateway (like the
# OpenClaw gateway) is re-ensured from a freshly spawned process at connection
# time. This launcher never blocks or fails the acp child.
set -u
: "${HERMES_HOME:=${HOME}/.hermes}"
log() { echo "hermes-gateway-ensure.sh: $*" 1>&2; }
# No channel configured -> nothing to run.
if [ -z "${SLACK_BOT_TOKEN:-}${TELEGRAM_BOT_TOKEN:-}" ]; then
  log "no SLACK_BOT_TOKEN/TELEGRAM_BOT_TOKEN in env; not starting gateway"
  exit 0
fi
PIDFILE="${HERMES_HOME}/gateway-ensure.pid"
# IMPORTANT: this guard must only short-circuit for a gateway WE launched in this
# live actor incarnation. The gateway is intentionally NOT pre-warmed into the
# golden snapshot (see buildAcpStartupScript): a snapshot-baked gateway would be
# restored with a dead Telegram long-poll TCP connection in a fresh network
# namespace, and this guard would then wrongly skip relaunching it. With no
# prewarm, the PID file is absent on first post-restore connection, so we launch
# a fresh gateway that dials Telegram in the live network.
if [ -f "${PIDFILE}" ] && kill -0 "$(cat "${PIDFILE}" 2>/dev/null)" 2>/dev/null; then
  log "gateway already running (pid $(cat "${PIDFILE}" 2>/dev/null)); nothing to do"
  exit 0
fi
mkdir -p "${HERMES_HOME}"
log "launching 'hermes gateway run' (TELEGRAM_BOT_TOKEN set: $([ -n "${TELEGRAM_BOT_TOKEN:-}" ] && echo yes || echo no), SLACK_BOT_TOKEN set: $([ -n "${SLACK_BOT_TOKEN:-}" ] && echo yes || echo no))"
# Send gateway output to this process's stderr (fd 2) so the actor log captures
# it: `kubectl ate logs actors <id>` then shows gateway startup/connect/errors.
# In the acp-shim child wrapper fd 1 is the ACP protocol stream (must stay clean
# JSON) while fd 2 is surfaced to the actor log, so 1>&2 moves gateway stdout off
# the ACP stream onto the log. (The actor's filesystem lives in an in-memory
# gVisor overlay, so a log file would not be readable from the host anyway.)
# No pipe, so $! is the hermes PID for the PID-file liveness guard above.
nohup hermes gateway run -v 1>&2 &
echo $! > "${PIDFILE}"
exit 0
