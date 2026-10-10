#!/bin/sh
set -eu
# Give the service user access to the mapped GPU node without changing host
# device permissions. The host render group number varies between distributions.
if [ "${GOSTREAM3D_RENDER_MODE:-cpu}" = nvidia ] && [ "${GOSTREAM3D_GPU_BACKEND:-nvidia}" = intel ]; then
    found=false
    for node in /dev/dri/renderD*; do
        [ -c "$node" ] || continue
        found=true
        gid=$(stat -c '%g' "$node")
        group=$(getent group "$gid" | cut -d: -f1 || true)
        if [ -z "$group" ]; then group="gpu-render-$gid"; groupadd -g "$gid" "$group"; fi
        usermod -a -G "$group" neko
    done
    if [ "$found" = false ]; then echo 'Intel GPU render device is missing' >&2; exit 1; fi
fi
exec "$@"
