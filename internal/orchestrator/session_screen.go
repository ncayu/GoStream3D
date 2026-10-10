package orchestrator

import "fmt"

func validSessionScreen(resolution string, fps int) bool {
	if fps != 30 && fps != 60 && fps != 90 && fps != 120 {
		return false
	}
	return resolution == "adaptive" || supportedScreens[fmt.Sprintf("%s@%d", resolution, fps)]
}
