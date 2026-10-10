package orchestrator

import (
	"encoding/json"
	"errors"
	"os"
)

func readCapacityReport(path string) (CapacityReport, error) {
	var p CapacityReport
	if path == "" {
		return p, errors.New("no report path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(data, &p)
	return p, err
}
func saveCapacityReport(path string, p CapacityReport) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return atomicStoreWrite(path, append(data, '\n'))
}
