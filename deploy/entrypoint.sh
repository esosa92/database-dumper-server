#!/bin/sh
set -e

if [ -d /host_ssh ]; then
    rm -rf /root/.ssh
    cp -r /host_ssh /root/.ssh
    chown -R root:root /root/.ssh
    chmod 700 /root/.ssh
    find /root/.ssh -type f -exec chmod 600 {} \;
fi

exec "$@"
