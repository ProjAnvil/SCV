package run

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// registryFile is the analysis-dir registry under the scv home (spec
// section 7): every run's analysis dir — including --output-dir runs
// outside ~/.scv/analysis — is appended there so the query surface can
// aggregate all registered roots.
const registryFile = "analysis-dirs.json"

// AppendAnalysisDir appends dir to the analysis-dirs registry at
// registryPath, creating the file when absent. The file is a JSON object
// {"dirs": ["<absolute dir>", ...]}; already-registered dirs are skipped
// (dedup) and the existing order is preserved. A registry that exists
// but does not parse is corruption and an error, never silently
// replaced.
func AppendAnalysisDir(registryPath, dir string) error {
	var reg struct {
		Dirs []string `json:"dirs"`
	}
	data, err := os.ReadFile(registryPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("read registry %s: %w", registryPath, err)
		}
	} else if err := json.Unmarshal(data, &reg); err != nil {
		return fmt.Errorf("parse registry %s: %w", registryPath, err)
	}
	for _, d := range reg.Dirs {
		if d == dir {
			return nil
		}
	}
	reg.Dirs = append(reg.Dirs, dir)
	out, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}
	if err := os.WriteFile(registryPath, append(out, '\n'), 0o644); err != nil {
		return fmt.Errorf("write registry %s: %w", registryPath, err)
	}
	return nil
}
