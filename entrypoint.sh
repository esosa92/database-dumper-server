#!/usr/bin/env bash

cp -R /usr/local/go/* /sdk/
command -v sshpass >/dev/null || (apt-get update -qq && apt-get install -y -qq sshpass >/dev/null)
command -v air >/dev/null || go install github.com/air-verse/air@latest
if [ -d /host_ssh ]; then
    rm -rf /root/.ssh
    cp -r /host_ssh /root/.ssh
    chown -R root:root /root/.ssh
    chmod 700 /root/.ssh
    find /root/.ssh -type f -exec chmod 600 {} \;
fi
exec bash
