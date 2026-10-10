#!/bin/sh
set -eu
umask 077
# Setup commands must work before any Docker socket or configuration is present.
if [ "$#" -gt 0 ]; then
    exec /app/gostream3d "$@"
fi
config_path=/data/config.env
if [ ! -f "$config_path" ]; then
    /app/gostream3d --config "$config_path" --init-config --setup-runtime docker \
        --public-ip "${GOSTREAM3D_ICE_PUBLIC_IP:-}"
fi
/app/gostream3d --config "$config_path" --doctor
exec /app/gostream3d --config "$config_path"
