//go:build !linux && !windows

package orchestrator

import "errors"

func NewNativeManager(_ *PortAllocator, _ NativeConfig) (WorkerController, error) {
	return nil, errors.New("native Neko runtime currently requires Linux and its Xorg/GStreamer dependencies")
}
