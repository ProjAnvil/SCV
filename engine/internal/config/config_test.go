package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// unknownKeysConfig mirrors the unknown-key surface of the repository-root
// config.example.json (lang, output_dir, batch_size, repos, parallel,
// fail_fast). The engine treats config files as an opaque superset, so all
// of these keys must be ignored without error.
const unknownKeysConfig = `{
  "lang": "en",
  "output_dir": "~/.scv/analysis",
  "batch_size": 5,
  "repos": [
    {"type": "remote", "url": "https://github.com/user/project1.git", "branch": "main"}
  ],
  "parallel": false,
  "fail_fast": false
}`

// writeConfig writes content to a scratch config file and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write scratch config: %v", err)
	}
	return path
}

// assertDefaults verifies the config.example.json defaults are in effect.
func assertDefaults(t *testing.T, cfg *Config) {
	t.Helper()
	if cfg == nil {
		t.Fatal("config is nil")
	}
	if want := []string{"springboot"}; !slices.Equal(cfg.StructureExtensions, want) {
		t.Errorf("StructureExtensions = %v, want %v", cfg.StructureExtensions, want)
	}
	wantLangs := []string{"java", "python", "javascript", "typescript", "go", "rust"}
	if !slices.Equal(cfg.StructureLanguages, wantLangs) {
		t.Errorf("StructureLanguages = %v, want %v", cfg.StructureLanguages, wantLangs)
	}
	if !cfg.StructureAnalysisEnabled {
		t.Error("StructureAnalysisEnabled = false, want true")
	}
	if cfg.DeepAnalysisEnabled {
		t.Error("DeepAnalysisEnabled = true, want false")
	}
	if cfg.LLM != nil {
		t.Errorf("LLM = %+v, want nil (no llm section means structure-only)", cfg.LLM)
	}
}

func TestLoadDefaults(t *testing.T) {
	path := writeConfig(t, `{}`)

	cfg, err := Load(path, "/scv-home")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	assertDefaults(t, cfg)
	if cfg.ScvHome != "/scv-home" {
		t.Errorf("ScvHome = %q, want %q", cfg.ScvHome, "/scv-home")
	}
	if cfg.Path != path {
		t.Errorf("Path = %q, want %q", cfg.Path, path)
	}
}

func TestMissingFileYieldsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.json")

	cfg, err := Load(path, "/scv-home")
	if err != nil {
		t.Fatalf("Load of a missing file must not error, got: %v", err)
	}
	assertDefaults(t, cfg)
	if cfg.Path != path {
		t.Errorf("Path = %q, want %q", cfg.Path, path)
	}
}

func TestUnknownKeysIgnored(t *testing.T) {
	path := writeConfig(t, unknownKeysConfig)

	cfg, err := Load(path, "/scv-home")
	if err != nil {
		t.Fatalf("Load with unknown keys returned error: %v", err)
	}
	assertDefaults(t, cfg)
}

func TestLLMSectionParsed(t *testing.T) {
	path := writeConfig(t, `{
  "llm": {"mode": "command", "command": ["claude", "-p"], "timeout_s": 600, "retry": 1}
}`)

	cfg, err := Load(path, "/scv-home")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.LLM == nil {
		t.Fatal("LLM is nil, want parsed section")
	}
	if cfg.LLM.Mode != "command" {
		t.Errorf("LLM.Mode = %q, want %q", cfg.LLM.Mode, "command")
	}
	if want := []string{"claude", "-p"}; !slices.Equal(cfg.LLM.Command, want) {
		t.Errorf("LLM.Command = %v, want %v", cfg.LLM.Command, want)
	}
	if cfg.LLM.TimeoutS != 600 {
		t.Errorf("LLM.TimeoutS = %d, want 600", cfg.LLM.TimeoutS)
	}
	if cfg.LLM.Retry != 1 {
		t.Errorf("LLM.Retry = %d, want 1", cfg.LLM.Retry)
	}
}

func TestLoadOverridesDefaults(t *testing.T) {
	path := writeConfig(t, `{
  "structure_extensions": ["maven"],
  "structure_analysis_enabled": false,
  "deep_analysis_enabled": true
}`)

	cfg, err := Load(path, "/scv-home")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if want := []string{"maven"}; !slices.Equal(cfg.StructureExtensions, want) {
		t.Errorf("StructureExtensions = %v, want %v", cfg.StructureExtensions, want)
	}
	if cfg.StructureAnalysisEnabled {
		t.Error("StructureAnalysisEnabled = true, want overridden false")
	}
	if !cfg.DeepAnalysisEnabled {
		t.Error("DeepAnalysisEnabled = false, want overridden true")
	}
	// Omitted keys keep their defaults.
	if want := []string{"java", "python", "javascript", "typescript", "go", "rust"}; !slices.Equal(cfg.StructureLanguages, want) {
		t.Errorf("StructureLanguages = %v, want default %v", cfg.StructureLanguages, want)
	}
}

func TestLoadInvalidJSONReturnsError(t *testing.T) {
	path := writeConfig(t, `{not json`)

	cfg, err := Load(path, "/scv-home")
	if err == nil {
		t.Fatal("Load of invalid JSON must return an error")
	}
	if cfg != nil {
		t.Errorf("Config = %+v, want nil on parse error", cfg)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q should mention the config path %q", err, path)
	}
}

func TestLoadUnreadablePathReturnsError(t *testing.T) {
	// A directory is a path that exists but cannot be read as a file.
	dir := t.TempDir()

	cfg, err := Load(dir, "/scv-home")
	if err == nil {
		t.Fatal("Load of an unreadable path must return an error")
	}
	if cfg != nil {
		t.Errorf("Config = %+v, want nil on read error", cfg)
	}
}

func TestLoadDoesNotCorruptDefaultsAcrossLoads(t *testing.T) {
	// json.Unmarshal reuses the backing array of an already-set slice, so
	// overrides must be decoded into per-Load copies of the defaults.
	if _, err := Load(writeConfig(t, `{"structure_extensions": ["maven"], "structure_languages": ["kotlin"]}`), "/scv-home"); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	cfg, err := Load(writeConfig(t, `{}`), "/scv-home")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	assertDefaults(t, cfg)
}

func TestSecretRefsCollectsRefShapedValues(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{
  "llm": {
    "mode": "command",
    "command": ["claude", "-p", "--api-key", "env:SCV_LLM_KEY", "file:/etc/scv/key"]
  },
  "structure_extensions": ["env:SHOULD_NOT_HAPPEN"]
}`), "/scv-home")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	// Refs may appear anywhere in the config; order is deterministic
	// (llm.mode, llm.command, extensions, languages).
	want := []string{"env:SCV_LLM_KEY", "file:/etc/scv/key", "env:SHOULD_NOT_HAPPEN"}
	if got := cfg.SecretRefs(); !slices.Equal(got, want) {
		t.Errorf("SecretRefs() = %v, want %v", got, want)
	}
}

func TestSecretRefsEmptyWithoutRefShapedValues(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{
  "llm": {"mode": "command", "command": ["claude", "-p"]}
}`), "/scv-home")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got := cfg.SecretRefs(); len(got) != 0 {
		t.Errorf("SecretRefs() = %v, want empty", got)
	}
}

func TestSecretRefsNilWithoutLLMSection(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.json"), "/scv-home")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got := cfg.SecretRefs(); len(got) != 0 {
		t.Errorf("SecretRefs() = %v, want empty when LLM is nil", got)
	}
}
