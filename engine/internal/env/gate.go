package env

import (
	"os"

	"github.com/projanvil/scv/engine/internal/config"
)

// The uniform diagnostics schema shared by `scv env` and the checking
// gate (spec section 7): every missing dependency is one Item with a
// severity and the capability it affects, so a host always gets the
// complete repair list in one shot.
const (
	StatusOK      = "ok"
	StatusMissing = "missing"

	SeverityBlocking  = "blocking"
	SeverityDegrading = "degrading"
	SeverityNone      = "none"

	CapabilityRun          = "run"
	CapabilityStructure    = "structure"
	CapabilityQuery        = "query"
	CapabilityDocGen       = "docgen"
	CapabilityDeepAnalysis = "deep-analysis"
)

// Item is one entry in the uniform diagnostics schema.
type Item struct {
	Item       string
	Status     string
	Severity   string
	Capability string
	Hint       string
}

// Mode selects the capabilities a run exercises; the gate classifies
// only the dependencies the requested capabilities actually need.
type Mode struct {
	Structure bool
	Query     bool
	DocGen    bool
}

// GateResult is the one-shot outcome of the unified dependency gate.
type GateResult struct {
	Blocking  []Item
	Degrading []Item
}

// Gate classifies every missing dependency of the run against the
// environment snapshot, the requested Mode, and the loaded config —
// the `checking` stage of spec section 7. Blocking items fail the run
// (ENV_INCOMPLETE with the full repair list); degrading items turn the
// corresponding capabilities off and are reported in one upfront
// `degraded` event.
//
// Blocking:
//   - python3 (the orchestrator runs perceive.py — always needed);
//   - git — controller ruling on the spec's "when repo verbs are
//     needed": every run mode prepares a repo (clone/link), so git is
//     blocking whenever ANY mode flag is set; a zero Mode runs nothing
//     and needs no repo verbs;
//   - perception root (the runtime must exist before any work starts);
//   - uv when Mode.Structure (structure output is expected).
//
// Degrading:
//   - go toolchain and prebuilt loader both absent — the query tier is
//     turned off. Controller ruling: this item fires regardless of mode
//     (the brief qualifies uv and docgen with mode flags and leaves the
//     go rule unqualified, like deep-analysis below). Only the loader
//     matters here: it produces the structure.vg the query tier
//     consumes, while the graphmcp binary serves external MCP consumers
//     (spec section 12, binary-OR-toolchain);
//   - llm unconfigured with Mode.DocGen — docgen is turned off;
//   - deep_analysis_enabled — unsupported in engine v1, regardless of
//     mode.
//
// A nil cfg behaves as zero config (structure-only defaults).
func (e *Env) Gate(m Mode, cfg *config.Config) GateResult {
	var result GateResult

	if toolMissing(e.Python3) {
		result.Blocking = append(result.Blocking, blockingItem("python3", CapabilityRun,
			"install python3 (3.9 or newer) — the orchestrator runs perceive.py"))
	}
	if (m.Structure || m.Query || m.DocGen) && toolMissing(e.Git) {
		result.Blocking = append(result.Blocking, blockingItem("git", CapabilityRun,
			"install git — https://git-scm.com/"))
	}
	if !dirExists(e.PerceptionRoot) {
		result.Blocking = append(result.Blocking, blockingItem("perception-root", CapabilityRun,
			"set SCV_PERCEPTION_ROOT or install the perception runtime under the scv home"))
	}
	if m.Structure && toolMissing(e.Uv) {
		result.Blocking = append(result.Blocking, blockingItem("uv", CapabilityStructure,
			"install uv — https://docs.astral.sh/uv/"))
	}

	if toolMissing(e.Go) && !e.PrebuiltLoader {
		result.Degrading = append(result.Degrading, degradingItem("go", CapabilityQuery,
			"install Go 1.27+ or ship the prebuilt dist binaries"))
	}
	if m.DocGen && (cfg == nil || cfg.LLM == nil) {
		result.Degrading = append(result.Degrading, degradingItem("docgen-unconfigured", CapabilityDocGen,
			"add an llm section to the config to enable document generation"))
	}
	if cfg != nil && cfg.DeepAnalysisEnabled {
		result.Degrading = append(result.Degrading, degradingItem("deep-analysis-unsupported", CapabilityDeepAnalysis,
			"deep analysis is not supported in engine v1; disable deep_analysis_enabled"))
	}

	return result
}

// blockingItem builds a blocking missing Item.
func blockingItem(item, capability, hint string) Item {
	return missingItem(item, capability, hint, SeverityBlocking)
}

// degradingItem builds a degrading missing Item.
func degradingItem(item, capability, hint string) Item {
	return missingItem(item, capability, hint, SeverityDegrading)
}

func missingItem(item, capability, hint, severity string) Item {
	return Item{
		Item:       item,
		Status:     StatusMissing,
		Severity:   severity,
		Capability: capability,
		Hint:       hint,
	}
}

// toolMissing reports whether a tool is absent or unusable; a present
// tool below its version floor counts as missing.
func toolMissing(t *Tool) bool {
	return t == nil || !t.OK
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
