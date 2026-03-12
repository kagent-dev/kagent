#!/usr/bin/env bash
set -e

# Create nginx temp directories
# These are required when running with readOnlyRootFilesystem: true
# The /tmp emptyDir volume is mounted empty at runtime, so we need to
# recreate the directory structure that was created during the Docker build
mkdir -p /tmp/nginx/client_temp \
         /tmp/nginx/proxy_temp \
         /tmp/nginx/fastcgi_temp \
         /tmp/nginx/uwsgi_temp \
         /tmp/nginx/scgi_temp

# Start supervisord
exec /usr/bin/supervisord -c /etc/supervisor/conf.d/supervisord.conf
