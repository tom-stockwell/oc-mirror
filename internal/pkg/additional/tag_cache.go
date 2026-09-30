package additional

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// tagCacheFilePath returns the cache file path for a given repository, laid
// out under a directory tree that mirrors the repository name itself (e.g.
// registry.example.com/team/tool/tags.json), so other kinds of per-image
// cached data can later live alongside it under the same per-repo directory.
func tagCacheFilePath(workingDir, repo string) string {
	return filepath.Join(workingDir, additionalImagesExtractDir, repo, "tags.json")
}

// writeTagListCache persists the resolved tag list for repo so a later
// diskToMirror (or delete) run can replay it without a live registry call.
func writeTagListCache(workingDir, repo string, tags []string) error {
	data, err := json.Marshal(tags)
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
	var tags []string
	if err := json.Unmarshal(data, &tags); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tag list cache for %q: %w", repo, err)
	}
	return tags, nil
}
