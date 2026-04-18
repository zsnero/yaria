package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// DownloadEntry is persisted to disk for resume across daemon restarts
type DownloadEntry struct {
	ID             string `json:"id"`
	URL            string `json:"url"`
	Title          string `json:"title"`
	Dir            string `json:"dir"`
	Paused         bool   `json:"paused"`
	IsAudioOnly    bool   `json:"is_audio_only,omitempty"`
	AudioFormat    string `json:"audio_format,omitempty"`
	Resolution     string `json:"resolution,omitempty"`
	CookieBrowser  string `json:"cookie_browser,omitempty"`
	UseAria2c      bool   `json:"use_aria2c,omitempty"`
	Aria2cArgs     string `json:"aria2c_args,omitempty"`
	OutputTemplate string `json:"output_template,omitempty"`
}

// DaemonState is the full persisted state
type DaemonState struct {
	Downloads []DownloadEntry `json:"downloads"`
}

// StateStore manages persistent daemon state
type StateStore struct {
	mu   sync.Mutex
	path string
	data DaemonState
}

// NewStateStore creates or loads state from the given directory
func NewStateStore(dir string) (*StateStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	s := &StateStore{path: filepath.Join(dir, "state.json")}
	s.load()
	return s, nil
}

func (s *StateStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &s.data)
}

// Save writes state to disk
func (s *StateStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(s.data, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

// AddDownload upserts a download entry
func (s *StateStore) AddDownload(entry DownloadEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.data.Downloads {
		if e.ID == entry.ID {
			s.data.Downloads[i] = entry
			return
		}
	}
	s.data.Downloads = append(s.data.Downloads, entry)
}

// RemoveDownload removes a download entry by ID
func (s *StateStore) RemoveDownload(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.data.Downloads {
		if e.ID == id {
			s.data.Downloads = append(s.data.Downloads[:i], s.data.Downloads[i+1:]...)
			return
		}
	}
}

// SetPaused sets the paused state of a download
func (s *StateStore) SetPaused(id string, paused bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.data.Downloads {
		if e.ID == id {
			s.data.Downloads[i].Paused = paused
			return
		}
	}
}

// GetDownloads returns a copy of all entries
func (s *StateStore) GetDownloads() []DownloadEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]DownloadEntry, len(s.data.Downloads))
	copy(out, s.data.Downloads)
	return out
}
