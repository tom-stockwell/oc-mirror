package additional

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type repoMetadata struct {
	Tags []string `json:"tags"`
}

// repoMetadataFilePath returns the metadata file path for a repository, under a
// directory tree mirroring the repository name (registry.example.com/team/tool/...).
func repoMetadataFilePath(workingDir, repo string) string {
	return filepath.Join(workingDir, additionalImagesExtractDir, repo, repoMetadataFileName)
}

// writeRepoMetadata persists the metadata for repo so a later diskToMirror
// (or delete) run can replay it without a live registry call.
func writeRepoMetadata(workingDir, repo string, meta repoMetadata) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata cache for %q: %w", repo, err)
	}
	path := repoMetadataFilePath(workingDir, repo)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create metadata cache dir for %q: %w", repo, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil { //nolint:gosec // no sensitive info
		return fmt.Errorf("failed to write metadata cache for %q: %w", repo, err)
	}
	return nil
}

// loadRepoMetadata reads back metadata written by writeRepoMetadata, erroring if
// none exists for repo - callers should treat that as "run mirrorToDisk first".
func loadRepoMetadata(workingDir, repo string) (repoMetadata, error) {
	path := repoMetadataFilePath(workingDir, repo)
	data, err := os.ReadFile(path) //nolint:gosec // path is derived internally, not user input
	if err != nil {
		if os.IsNotExist(err) {
			return repoMetadata{}, fmt.Errorf("no cached tag list found for repository %q: run mirrorToDisk (or mirrorToMirror) first", repo)
		}
		return repoMetadata{}, fmt.Errorf("failed to read metadata cache for %q: %w", repo, err)
	}
	var meta repoMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return repoMetadata{}, fmt.Errorf("failed to unmarshal metadata cache for %q: %w", repo, err)
	}
	return meta, nil
}
