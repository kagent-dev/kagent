# Fetching over HTTP

WebFetch answers an HTTP error with its status code alone: the status text and
the body, where a server or this agent's egress gateway says why, are dropped.

When WebFetch reports an error status for a URL, fetch the same URL in the
shell with `curl -sS -i --max-time 30 '<url>'` and read the status line and
body. A refusal by the egress gateway names the refused host and says it is not
in this agent's egress policy, which the agent's author can change in the
agent's egress settings. Answer with that reason, the host included, rather
than guessing why the server refused.
