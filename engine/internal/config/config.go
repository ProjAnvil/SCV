// Package config loads SCV engine configuration from JSON config files.
//
// Config files follow the shape of config.example.json at the repository
// root; the engine reads only the keys it needs and ignores everything
// else, so files written for other SCV components stay valid here.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
)

// LLM holds the command-mode doc generation settings (spec section 8).
// A nil LLM in Config means no llm section is configured and the run is
// structure-only (docgen reports degraded instead of failing).
type LLM struct {
	Mode     string   `json:"mode"`
	Command  []string `json:"command"`
	TimeoutS int      `json:"timeout_s"`
	Retry    int      `json:"retry"`
}

// Config is the engine view of an SCV config file. ScvHome and Path are
// engine inputs recorded for downstream use; they are never loaded from
// the file itself.
type Config struct {
	ScvHome string `json:"-"`
	Path    string `json:"-"`

	LLM *LLM `json:"llm"`

	StructureExtensions      []string `json:"structure_extensions"`
	StructureLanguages       []string `json:"structure_languages"`
	StructureAnalysisEnabled bool     `json:"structure_analysis_enabled"`
	DeepAnalysisEnabled      bool     `json:"deep_analysis_enabled"`
}

// Defaults mirroring config.example.json at the repository root.
const (
	defaultStructureAnalysisEnabled = true
	defaultDeepAnalysisEnabled      = false
)

var (
	defaultStructureExtensions = []string{"springboot"}
	defaultStructureLanguages  = []string{"java", "python", "javascript", "typescript", "go", "rust"}
)

// Load reads the config file at path and returns a Config with defaults
// applied for every field the file omits. A missing file is not an error:
// the defaults-only Config is returned so a fresh install works with no
// config at all. Unknown JSON keys are ignored by design (config files are
// shared with other SCV components). A file that exists but cannot be read
// or parsed is an error. scvHome is recorded as-is; resolving it from the
// environment is the env package's job.
func Load(path, scvHome string) (*Config, error) {
	// The slice defaults are cloned per call: json.Unmarshal reuses the
	// backing array of an already-set slice, so sharing the package-level
	// slices here would let one Load corrupt the defaults of every later
	// Load.
	cfg := &Config{
		ScvHome:                  scvHome,
		Path:                     path,
		StructureExtensions:      slices.Clone(defaultStructureExtensions),
		StructureLanguages:       slices.Clone(defaultStructureLanguages),
		StructureAnalysisEnabled: defaultStructureAnalysisEnabled,
		DeepAnalysisEnabled:      defaultDeepAnalysisEnabled,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	// json.Unmarshal only overwrites fields present in the file, so the
	// defaults above survive for omitted keys. Unknown fields are skipped
	// because DisallowUnknownFields stays disabled by design.
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

// SecretRefs returns every config value shaped like a secret reference,
// i.e. with an "env:VAR" or "file:/path" prefix. The engine passes such
// values around as references only and never dereferences, stores, or
// logs the secrets behind them (spec section 8, key custody). Refs with a
// malformed tail are returned as-is: validation is warn-only and belongs
// to the caller. ScvHome and Path are engine inputs, not config-file
// values, so they are never treated as refs.
func (c *Config) SecretRefs() []string {
	var refs []string
	for _, v := range c.stringValues() {
		if strings.HasPrefix(v, "env:") || strings.HasPrefix(v, "file:") {
			refs = append(refs, v)
		}
	}
	return refs
}

// stringValues enumerates every string value loaded from the config file
// in a deterministic order: llm.mode, llm.command, structure extensions,
// structure languages.
func (c *Config) stringValues() []string {
	values := make([]string, 0, 1+len(c.StructureExtensions)+len(c.StructureLanguages))
	if c.LLM != nil {
		values = append(values, c.LLM.Mode)
		values = append(values, c.LLM.Command...)
	}
	values = append(values, c.StructureExtensions...)
	values = append(values, c.StructureLanguages...)
	return values
}
