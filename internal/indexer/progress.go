package indexer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ProgressEvent is emitted by IndexAll to report progress.
type ProgressEvent struct {
	Phase          string // "scanning", "indexing"
	TotalFiles     int
	ProcessedFiles int
	CurrentFile    string
	Chunks         int
	FailedFiles    int
}

// ProgressCallback receives progress events during indexing.
// A nil callback disables progress reporting.
type ProgressCallback func(ProgressEvent)

// ProgressState is the JSON structure written to the state file.
type ProgressState struct {
	Running        bool      `json:"running"`
	Phase          string    `json:"phase"`
	TotalFiles     int       `json:"total_files"`
	ProcessedFiles int       `json:"processed_files"`
	CurrentFile    string    `json:"current_file"`
	Chunks         int       `json:"chunks"`
	FailedFiles    int       `json:"failed_files"`
	StartedAt      time.Time `json:"started_at"`
	Percent        float64   `json:"percent"`
}

const indexStateFileName = "index.state.json"

var progressWriteMu sync.Mutex

// WriteProgressState writes the state file atomically using temp+rename.
func WriteProgressState(codelensDir string, state ProgressState) error {
	progressWriteMu.Lock()
	defer progressWriteMu.Unlock()

	if err := os.MkdirAll(codelensDir, 0755); err != nil {
		return err
	}

	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(codelensDir, indexStateFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadProgressState reads the current index state file.
func ReadProgressState(codelensDir string) (ProgressState, error) {
	var s ProgressState
	b, err := os.ReadFile(filepath.Join(codelensDir, indexStateFileName))
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	return s, nil
}
