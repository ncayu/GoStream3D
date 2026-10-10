#!/bin/sh
set -eu

if [ -z "${GOSTREAM3D_TARGET_URL:-}" ]; then
    echo "GOSTREAM3D_TARGET_URL is required" >&2
    exit 1
fi

screen_size="${NEKO_SCREEN:-1920x1080@30}"
screen_size="${screen_size%@*}"
initial_url="${GOSTREAM3D_TARGET_URL}"
if [ "${GOSTREAM3D_BROWSER_BOOTSTRAP:-false}" = "true" ]; then
    initial_url="about:blank"
fi
set -- \
    --no-sandbox \
    --window-position=0,0 \
    --window-size="${screen_size%x*},${screen_size#*x}" \
    --force-device-scale-factor=1 \
    --display="${DISPLAY:-:0}" \
    --user-data-dir=/home/neko/.config/chromium \
    --no-first-run \
    --start-maximized \
    --kiosk \
    --disable-dev-shm-usage \
    --remote-debugging-address=0.0.0.0 \
    --remote-debugging-port="${GOSTREAM3D_CHROMIUM_CDP_PORT:-9222}" \
    --ignore-gpu-blocklist \
    --enable-webgl \
    --app="${initial_url}"

case "${GOSTREAM3D_RENDER_MODE:-cpu}" in
    cpu)
        exec /usr/bin/chromium "$@" \
            --use-gl=angle \
            --use-angle=swiftshader \
            --enable-unsafe-swiftshader
        ;;
    nvidia)
        case "${GOSTREAM3D_GPU_GRAPHICS_API:-vulkan}" in
            opengl)
                exec /usr/bin/chromium "$@" \
                    --use-gl=angle \
                    --use-angle=gl \
                    --disable-software-rasterizer
                ;;
            vulkan) ;;
            *)
                echo "unsupported GPU graphics API" >&2
                exit 1
                ;;
        esac
        exec /usr/bin/chromium "$@" \
            --use-gl=angle \
            --enable-features=Vulkan \
            --use-angle=vulkan \
            --disable-vulkan-surface \
            --disable-software-rasterizer
        ;;
    *)
        echo "unsupported render mode" >&2
        exit 1
        ;;
esac
