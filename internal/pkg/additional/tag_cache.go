package additional

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// tagListCacheEntry is the on-disk shape of a cached repository tag list.
// Repo is stored alongside Tags purely for human-debuggability of the cache
// file; it is not used to validate the lookup key.
type tagListCacheEntry struct {
	Repo string   `json:"repo"`
	Tags []string `json:"tags"`
}

// tagCacheFilePath returns the cache file path for a given repository,
// hashing the repo name to keep the filename filesystem-safe regardless of
// what characters the repo reference contains.
func tagCacheFilePath(workingDir, repo string) string {
	sum := sha256.Sum256([]byte(repo))
	return filepath.Join(workingDir, additionalImagesExtractDir, tagListCacheDir, hex.EncodeToString(sum[:16])+".json")
}

// writeTagListCache persists the resolved tag list for repo so a later
// diskToMirror (or delete) run can replay it without a live registry call.
func writeTagListCache(workingDir, repo string, tags []string) error {
	entry := tagListCacheEntry{Repo: repo, Tags: tags}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal tag list cache for %q: %w", repo, err)
	}
	path := tagCacheFilePath(workingDir, repo)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create tag list cache dir for %q: %w", repo, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil { //nolint:gosec // no sensitive info
		return fmt.Errorf("failed to write tag list cache for %q: %w", repo, err)
	}
	return nil
}

// loadTagListCache reads back a tag list previously written by
// writeTagListCache. It returns an error if no cache exists for repo -
// callers should treat that as "run mirrorToDisk first".
func loadTagListCache(workingDir, repo string) ([]string, error) {
	path := tagCacheFilePath(workingDir, repo)
	data, err := os.ReadFile(path) //nolint:gosec // path is derived internally, not user input
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no cached tag list found for repository %q: run mirrorToDisk (or mirrorToMirror) first", repo)
		}
		return nil, fmt.Errorf("failed to read tag list cache for %q: %w", repo, err)
	}
	var entry tagListCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tag list cache for %q: %w", repo, err)
	}
	return entry.Tags, nil
}
