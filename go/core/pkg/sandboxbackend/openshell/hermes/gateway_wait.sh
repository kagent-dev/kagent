listen='127.0.0.1:{{ .Port }}'

if ! command -v ss >/dev/null 2>&1; then
  echo 'hermes gateway wait: ss not found in PATH' >&2
  exit 2
fi

for i in $(seq 1 30); do
  if ss -tln 2>/dev/null | grep -qF "$listen"; then
    exit 0
  fi
  sleep 1
done

echo "hermes gateway wait: timed out after 30s waiting for $listen" >&2
echo '--- ss -tln ---' >&2
ss -tln 2>&1 | head -20 >&2 || true
echo '--- tail /tmp/gateway.log ---' >&2
tail -n 40 /tmp/gateway.log 2>&1 >&2 || echo '(no /tmp/gateway.log)' >&2
exit 1
