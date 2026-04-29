package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

var bucketDownloads = []byte("downloads")

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

// DaemonState is kept for JSON migration compatibility.
type DaemonState struct {
	Downloads []DownloadEntry `json:"downloads"`
}

// StateStore manages persistent daemon state using BoltDB.
type StateStore struct {
	mu sync.Mutex
	db *bolt.DB
}

// NewStateStore creates or loads state from the given directory.
func NewStateStore(dir string) (*StateStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dir, "state.db")
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open state db: %w", err)
	}

	// Create bucket
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketDownloads)
		return err
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	s := &StateStore{db: db}

	// Migrate from old JSON state if it exists
	s.migrateFromJSON(dir)

	return s, nil
}

// migrateFromJSON imports data from the old state.json file if it exists.
func (s *StateStore) migrateFromJSON(dir string) {
	jsonPath := filepath.Join(dir, "state.json")
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return // no old file
	}

	var old DaemonState
	if err := json.Unmarshal(data, &old); err != nil {
		return
	}

	for _, entry := range old.Downloads {
		s.AddDownload(entry)
	}

	// Rename old file so it's not re-imported
	os.Rename(jsonPath, jsonPath+".migrated")
}

// Save is a no-op -- BoltDB auto-persists on each Update transaction.
// Kept for API compatibility with the Manager.
func (s *StateStore) Save() error {
	return nil
}

// Close closes the BoltDB database.
func (s *StateStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// AddDownload upserts a download entry.
func (s *StateStore) AddDownload(entry DownloadEntry) {
	s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketDownloads)
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return b.Put([]byte(entry.ID), data)
	})
}

// RemoveDownload removes a download entry by ID.
func (s *StateStore) RemoveDownload(id string) {
	s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketDownloads).Delete([]byte(id))
	})
}

// SetPaused sets the paused state of a download.
func (s *StateStore) SetPaused(id string, paused bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketDownloads)
		data := b.Get([]byte(id))
		if data == nil {
			return nil
		}
		var entry DownloadEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			return err
		}
		entry.Paused = paused
		updated, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return b.Put([]byte(id), updated)
	})
}

// GetDownloads returns a copy of all entries.
func (s *StateStore) GetDownloads() []DownloadEntry {
	var entries []DownloadEntry
	s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketDownloads)
		return b.ForEach(func(k, v []byte) error {
			var entry DownloadEntry
			if err := json.Unmarshal(v, &entry); err != nil {
				return nil // skip corrupted entries
			}
			entries = append(entries, entry)
			return nil
		})
	})
	return entries
}
