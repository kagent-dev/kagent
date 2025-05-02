ARG VARIANT="focal"
FROM buildpack-deps:${VARIANT}-curl

LABEL dev.containers.features="common"

COPY first-run-notice.txt /tmp/scripts/

# Move first run notice to right spot
RUN mkdir -p "/usr/local/etc/vscode-dev-containers/" \
    && mv -f /tmp/scripts/first-run-notice.txt /usr/local/etc/vscode-dev-containers/

# Remove scripts now that we're done with them
RUN rm -rf /tmp/scripts
