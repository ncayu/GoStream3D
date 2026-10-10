package orchestrator

import "time"

// NativeConfig describes binaries already installed on the Linux host.
// The Go gateway manages their lifecycle; no Docker daemon is involved.
type NativeConfig struct {
	NekoBinary      string
	ChromiumBinary  string
	CPURenderer     string
	CPUEffects      string
	XorgBinary      string
	PulseBinary     string
	VirtualGLBinary string
	GPUBackend      string
	GPURenderDevice string
	WorkRoot        string
	WebDist         string
	ICEPublicIP     string
	UserPassword    string
	AdminPassword   string
	ReadyTimeout    time.Duration
}
