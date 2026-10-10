package orchestrator

import "encoding/json"

// Configure both streams explicitly: Neko otherwise rewrites a single custom
// pipeline to show-pointer=true for legacy clients, duplicating the local cursor.
func videoPipelineEnvironment(pipeline string) []string {
	config := map[string]map[string]string{
		"main":   {"gst_pipeline": pipeline},
		"legacy": {"gst_pipeline": pipeline},
	}
	encoded, _ := json.Marshal(config)
	return []string{"NEKO_CAPTURE_VIDEO_PIPELINES=" + string(encoded), "NEKO_CAPTURE_VIDEO_IDS=main"}
}
