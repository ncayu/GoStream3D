//go:build windows

package orchestrator

import _ "embed"

// Installed only for explicitly selected CPU simplification, before navigation.
//
//go:embed cpu_effects.js
var cpuEffectsScript string
