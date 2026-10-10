#!/bin/sh
set -eu
set -f

# Xorg in this worker has no window manager. Chromium's maximize/kiosk hints
# therefore do not resize its app window, including after RandR screen changes.
# Keep the main browser window aligned with the desktop without changing zoom
# or emulating a viewport that the video capture cannot see.
export DISPLAY="${DISPLAY:-:99.0}"
while :; do
    sleep 1
    display_size=$(xdotool getdisplaygeometry 2>/dev/null) || continue
    set -- $display_size
    [ "$#" -eq 2 ] || continue
    width=$1 height=$2
    case "$width:$height" in *[!0-9:]*|0:*|*:0) continue ;; esac
    windows=$(xdotool search --onlyvisible --class '^Chromium$' 2>/dev/null) || continue
    for window in $windows; do
        # Do not resize Chromium menus, tooltips or file picker dialogs.
        kind=$(xprop -id "$window" _NET_WM_WINDOW_TYPE 2>/dev/null) || continue
        case "$kind" in *"_NET_WM_WINDOW_TYPE_NORMAL"*) ;; *) continue ;; esac
        geometry=$(xdotool getwindowgeometry --shell "$window" 2>/dev/null) || continue
        case "$geometry" in *"
X=0
Y=0
WIDTH=$width
HEIGHT=$height
SCREEN="*) continue ;; esac
        xdotool windowmove "$window" 0 0 windowsize "$window" "$width" "$height" 2>/dev/null || true
    done
done
