package updater

import (
	"crypto/sha256"
	"fmt"
)

// Fingerprint computes a SHA-256 fingerprint of the given content.
func Fingerprint(content []byte) string {
	h := sha256.Sum256(content)
	return fmt.Sprintf("%x", h)
}

// FingerprintStore manages stored fingerprints for incremental updates.
type FingerprintStore struct {
	fingerprints map[string]string // path → hash
}

// NewFingerprintStore creates an empty fingerprint store.
func NewFingerprintStore() *FingerprintStore {
	return &FingerprintStore{fingerprints: make(map[string]string)}
}

// Set records a fingerprint for a path.
func (fs *FingerprintStore) Set(path, hash string) {
	fs.fingerprints[path] = hash
}

// Get returns the stored fingerprint for a path.
func (fs *FingerprintStore) Get(path string) (string, bool) {
	h, ok := fs.fingerprints[path]
	return h, ok
}

// Compare compares current fingerprints against stored ones.
// Returns lists of new, changed, and removed paths.
func (fs *FingerprintStore) Compare(current map[string]string) (newPaths, changed, removed []string) {
	// Find new and changed
	for path, hash := range current {
		stored, exists := fs.fingerprints[path]
		if !exists {
			newPaths = append(newPaths, path)
		} else if stored != hash {
			changed = append(changed, path)
		}
	}

	// Find removed
	for path := range fs.fingerprints {
		if _, exists := current[path]; !exists {
			removed = append(removed, path)
		}
	}

	return
}

// LoadFromDB populates the store from the database fingerprints table.
func (fs *FingerprintStore) LoadFromDB(rows []FingerprintRow) {
	for _, row := range rows {
		fs.fingerprints[row.FilePath] = row.Hash
	}
}

// FingerprintRow represents a row from the fingerprints table.
type FingerprintRow struct {
	SourceID string
	FilePath string
	Hash     string
}
