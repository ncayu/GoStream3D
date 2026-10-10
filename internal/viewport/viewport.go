// Package viewport holds the bounded viewport protocol shared by renderers.
package viewport

import (
	"context"
	"net/http"
)

const Adaptive = "adaptive"

type Size struct {
	Type   string `json:"type"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Encoder-friendly dimensions also match Neko's Xorg width alignment.
func (s Size) Valid() bool {
	return s.Width >= 256 && s.Width <= 3840 && s.Height >= 256 && s.Height <= 2160 && s.Width%8 == 0 && s.Height%2 == 0
}

type settingsKey struct{}
type Settings struct {
	Adaptive bool
	FPS      int
}

// Only the gateway's acquired, independent worker can receive viewport changes.
func WithSettings(r *http.Request, adaptive bool, fps int) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), settingsKey{}, Settings{Adaptive: adaptive, FPS: fps}))
}

func FromRequest(r *http.Request) Settings {
	s, _ := r.Context().Value(settingsKey{}).(Settings)
	return s
}
