package run

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// metadataFile is the per-analysis-dir metadata file the Python
// toolchain already reads (scv_util.py get_metadata_path).
const metadataFile = ".scv_metadata.json"

// scvVersion is the version stamp scv_util.py write-metadata records;
// ported verbatim so Go- and Python-written metadata are interchangeable.
const scvVersion = "2.0"

// docArtifacts are the four documents a docgen-configured run expects
// (spec section 8); the skip check requires all of them.
var docArtifacts = []string{"README.md", "SUMMARY.md", "ARCHITECTURE.md", "FILE_INDEX.md"}

// WriteMetadata writes the analysis metadata for a completed run — a
// port of scv_util.py write-metadata. Keys: last_analyzed_commit,
// last_analyzed_at, created_at, repo_path, scv_version. It is additive:
// created_at is preserved across rewrites and keys the engine does not
// own survive (a superset of the Python port, which only preserves
// created_at). commit may be empty (non-git sources carry no commit
// identity).
func WriteMetadata(analysisDir, repoPath, commit string) error {
	if err := os.MkdirAll(analysisDir, 0o755); err != nil {
		return fmt.Errorf("create analysis dir: %w", err)
	}
	existing := map[string]any{}
	if data, err := os.ReadFile(filepath.Join(analysisDir, metadataFile)); err == nil {
		// An unparseable file starts fresh, exactly like scv_util.py's
		// read_metadata returning None.
		_ = json.Unmarshal(data, &existing)
	}
	createdAt, _ := existing["created_at"].(string)
	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	meta := map[string]any{
		"last_analyzed_commit": commit,
		"last_analyzed_at":     time.Now().UTC().Format(time.RFC3339Nano),
		"created_at":           createdAt,
		"repo_path":            repoPath,
		"scv_version":          scvVersion,
	}
	for k, v := range existing {
		if _, owned := meta[k]; !owned {
			meta[k] = v
		}
	}
	out, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(analysisDir, metadataFile), append(out, '\n'), 0o644); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	return nil
}

// ShouldSkip reports whether a --skip-unchanged run may skip the commit:
// the last analyzed commit matches AND structure.json exists AND, when
// docGen is set, all four documents exist (spec section 7, audit Q7 — a
// structure-only past run never traps a later docgen-configured run).
// An empty commit never skips: non-git sources have no commit identity
// to match.
func ShouldSkip(analysisDir, commit string, docGen bool) bool {
	if commit == "" {
		return false
	}
	meta := readMetadata(analysisDir)
	if meta == nil {
		return false
	}
	last, _ := meta["last_analyzed_commit"].(string)
	if last == "" || last != commit {
		return false
	}
	if !fileExists(filepath.Join(analysisDir, "structure.json")) {
		return false
	}
	if docGen {
		for _, doc := range docArtifacts {
			if !fileExists(filepath.Join(analysisDir, doc)) {
				return false
			}
		}
	}
	return true
}

// readMetadata loads .scv_metadata.json; nil when absent or unparseable
// (scv_util.py read_metadata semantics).
func readMetadata(analysisDir string) map[string]any {
	data, err := os.ReadFile(filepath.Join(analysisDir, metadataFile))
	if err != nil {
		return nil
	}
	var meta map[string]any
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil
	}
	return meta
}
