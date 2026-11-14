package env

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/projanvil/scv/engine/internal/config"
)

// completeEnv returns an Env whose gate-relevant dependencies are all
// satisfied; tests nil out or falsify fields to stage specific misses.
// Gate is a pure function over the snapshot, so no discovery is needed
// here and no assertion depends on the host machine.
func completeEnv(t *testing.T) *Env {
	t.Helper()
	return &Env{
		PerceptionRoot:   t.TempDir(),
		Uv:               &Tool{Path: "/fake/uv", Version: "uv 0.9.9", OK: true},
		Python3:          &Tool{Path: "/fake/python3", OK: true},
		Git:              &Tool{Path: "/fake/git", OK: true},
		Go:               &Tool{Path: "/fake/go", Version: "go1.27.0", OK: true},
		Java:             &Tool{Path: "/fake/java", OK: true},
		PrebuiltLoader:   true,
		PrebuiltGraphMCP: true,
	}
}

func itemNames(items []Item) []string {
	names := make([]string, len(items))
	for i, item := range items {
		names[i] = item.Item
	}
	return names
}

func findItem(t *testing.T, items []Item, name string) Item {
	t.Helper()
	for _, item := range items {
		if item.Item == name {
			return item
		}
	}
	t.Fatalf("item %q not found in %v", name, itemNames(items))
	return Item{}
}

func TestGateBlockingCombinesAllMissing(t *testing.T) {
	t.Run("every blocking miss is reported at once", func(t *testing.T) {
		env := &Env{PerceptionRoot: filepath.Join(t.TempDir(), "absent-perception")}

		res := env.Gate(Mode{Structure: true, Query: true, DocGen: true}, &config.Config{})

		// Spec section 7: one failure, complete repair list — never
		// one-at-a-time. Order is deterministic.
		want := []string{"python3", "git", "perception-root", "uv"}
		if !slices.Equal(itemNames(res.Blocking), want) {
			t.Fatalf("Blocking = %v, want %v", itemNames(res.Blocking), want)
		}
		for _, item := range res.Blocking {
			if item.Status != StatusMissing || item.Severity != SeverityBlocking {
				t.Errorf("item %q: status/severity = %q/%q, want missing/blocking", item.Item, item.Status, item.Severity)
			}
			if item.Capability == "" || item.Hint == "" {
				t.Errorf("item %q: capability and hint must be filled", item.Item)
			}
		}
		// The same one-shot principle applies to Degrading: go and the
		// prebuilt loader are both absent here, so the query degradation
		// rides along in the same gate result.
		if item := findItem(t, res.Degrading, "go"); item.Capability != CapabilityQuery {
			t.Errorf("degrading go capability = %q, want %q", item.Capability, CapabilityQuery)
		}
	})

	t.Run("complete environment passes clean", func(t *testing.T) {
		env := completeEnv(t)
		cfg := &config.Config{LLM: &config.LLM{Mode: "command", Command: []string{"claude", "-p"}}}

		res := env.Gate(Mode{Structure: true, Query: true, DocGen: true}, cfg)

		if len(res.Blocking) != 0 || len(res.Degrading) != 0 {
			t.Fatalf("gate = %+v, want empty Blocking and Degrading", res)
		}
	})
}

func TestGateDegradingQueryOff(t *testing.T) {
	t.Run("go toolchain and prebuilt loader both absent", func(t *testing.T) {
		env := completeEnv(t)
		env.Go = nil
		env.PrebuiltLoader = false

		res := env.Gate(Mode{Query: true}, &config.Config{})

		if want := []string{"go"}; !slices.Equal(itemNames(res.Degrading), want) {
			t.Fatalf("Degrading = %v, want %v", itemNames(res.Degrading), want)
		}
		item := findItem(t, res.Degrading, "go")
		if item.Capability != CapabilityQuery || item.Severity != SeverityDegrading || item.Status != StatusMissing {
			t.Errorf("go item = %+v, want degrading/missing with capability query", item)
		}
		if len(res.Blocking) != 0 {
			t.Errorf("Blocking = %v, want empty", itemNames(res.Blocking))
		}
	})

	t.Run("prebuilt loader substitutes for the toolchain", func(t *testing.T) {
		env := completeEnv(t)
		env.Go = nil
		env.PrebuiltLoader = true

		res := env.Gate(Mode{Query: true}, &config.Config{})

		if len(res.Degrading) != 0 {
			t.Errorf("Degrading = %v, want empty when dist carries the prebuilt loader", itemNames(res.Degrading))
		}
	})

	t.Run("go present at minimum version", func(t *testing.T) {
		env := completeEnv(t)
		env.PrebuiltLoader = false

		res := env.Gate(Mode{Query: true}, &config.Config{})

		if len(res.Degrading) != 0 {
			t.Errorf("Degrading = %v, want empty when the toolchain is present", itemNames(res.Degrading))
		}
	})

	t.Run("degrades regardless of requested mode", func(t *testing.T) {
		// Controller ruling: unlike uv (Structure) and docgen (DocGen),
		// the go/dist rule is unqualified — the item fires for any mode,
		// including a zero Mode.
		for _, mode := range []Mode{{Structure: true}, {}, {DocGen: true}} {
			env := completeEnv(t)
			env.Go = nil
			env.PrebuiltLoader = false
			// LLM configured so the DocGen case isolates the go item.
			cfg := &config.Config{LLM: &config.LLM{Mode: "command"}}

			res := env.Gate(mode, cfg)

			if want := []string{"go"}; !slices.Equal(itemNames(res.Degrading), want) {
				t.Errorf("Degrading for mode %+v = %v, want %v (mode-independent)", mode, itemNames(res.Degrading), want)
			}
			if item := findItem(t, res.Degrading, "go"); item.Capability != CapabilityQuery {
				t.Errorf("go capability for mode %+v = %q, want %q", mode, item.Capability, CapabilityQuery)
			}
		}
	})

	t.Run("go below minimum counts as missing", func(t *testing.T) {
		env := completeEnv(t)
		env.Go = &Tool{Path: "/fake/go", Version: "go1.21.5", OK: false}
		env.PrebuiltLoader = false

		res := env.Gate(Mode{Query: true}, &config.Config{})

		if want := []string{"go"}; !slices.Equal(itemNames(res.Degrading), want) {
			t.Fatalf("Degrading = %v, want %v", itemNames(res.Degrading), want)
		}
	})
}

func TestGateDegradingDocgenUnconfigured(t *testing.T) {
	t.Run("llm section absent with docgen mode", func(t *testing.T) {
		env := completeEnv(t)

		res := env.Gate(Mode{DocGen: true}, &config.Config{})

		if want := []string{"docgen-unconfigured"}; !slices.Equal(itemNames(res.Degrading), want) {
			t.Fatalf("Degrading = %v, want %v", itemNames(res.Degrading), want)
		}
		item := findItem(t, res.Degrading, "docgen-unconfigured")
		if item.Capability != CapabilityDocGen || item.Severity != SeverityDegrading {
			t.Errorf("docgen item = %+v, want degrading with capability docgen", item)
		}
	})

	t.Run("llm configured keeps docgen on", func(t *testing.T) {
		env := completeEnv(t)
		cfg := &config.Config{LLM: &config.LLM{Mode: "command", Command: []string{"claude", "-p"}}}

		res := env.Gate(Mode{DocGen: true}, cfg)

		if len(res.Degrading) != 0 {
			t.Errorf("Degrading = %v, want empty", itemNames(res.Degrading))
		}
	})

	t.Run("nil config counts as unconfigured", func(t *testing.T) {
		env := completeEnv(t)

		res := env.Gate(Mode{DocGen: true}, nil)

		if want := []string{"docgen-unconfigured"}; !slices.Equal(itemNames(res.Degrading), want) {
			t.Fatalf("Degrading = %v, want %v (nil cfg = zero config, no deep item either)", itemNames(res.Degrading), want)
		}
	})
}

func TestGateDeepAnalysisUnsupported(t *testing.T) {
	env := completeEnv(t)
	cfg := &config.Config{DeepAnalysisEnabled: true}

	// Fires regardless of mode: it is a config promise v1 cannot keep.
	res := env.Gate(Mode{}, cfg)

	if want := []string{"deep-analysis-unsupported"}; !slices.Equal(itemNames(res.Degrading), want) {
		t.Fatalf("Degrading = %v, want %v", itemNames(res.Degrading), want)
	}
	item := findItem(t, res.Degrading, "deep-analysis-unsupported")
	if item.Capability != CapabilityDeepAnalysis || item.Hint == "" {
		t.Errorf("deep-analysis item = %+v, want capability and hint filled", item)
	}
}

func TestGateUvBlockingRequiresStructureMode(t *testing.T) {
	env := completeEnv(t)
	env.Uv = nil

	if res := env.Gate(Mode{Query: true}, &config.Config{}); len(res.Blocking) != 0 {
		t.Errorf("Blocking = %v, want empty: uv blocks only when structure output is expected", itemNames(res.Blocking))
	}

	res := env.Gate(Mode{Structure: true}, &config.Config{})
	if want := []string{"uv"}; !slices.Equal(itemNames(res.Blocking), want) {
		t.Fatalf("Blocking = %v, want %v", itemNames(res.Blocking), want)
	}
	item := findItem(t, res.Blocking, "uv")
	if item.Capability != CapabilityStructure {
		t.Errorf("uv capability = %q, want %q", item.Capability, CapabilityStructure)
	}
	if want := "install uv — https://docs.astral.sh/uv/"; item.Hint != want {
		t.Errorf("uv hint = %q, want the spec section 7 hint %q", item.Hint, want)
	}
}

func TestGateGitBlockingNeedsRepoPrep(t *testing.T) {
	// Controller ruling: all run modes prepare a repo (clone/link), so
	// git is blocking whenever ANY mode flag is set; a zero Mode runs
	// nothing and needs no repo verbs.
	t.Run("zero mode needs no repo verbs", func(t *testing.T) {
		env := completeEnv(t)
		env.Git = nil

		if res := env.Gate(Mode{}, &config.Config{}); len(res.Blocking) != 0 {
			t.Errorf("Blocking = %v, want empty for zero mode", itemNames(res.Blocking))
		}
	})

	for _, mode := range []Mode{
		{Structure: true},
		{Query: true},
		{DocGen: true},
	} {
		t.Run("git blocks when any capability runs", func(t *testing.T) {
			env := completeEnv(t)
			env.Git = nil

			res := env.Gate(mode, &config.Config{})

			if want := []string{"git"}; !slices.Equal(itemNames(res.Blocking), want) {
				t.Fatalf("Blocking for mode %+v = %v, want %v", mode, itemNames(res.Blocking), want)
			}
		})
	}
}

func TestJavaInformationalNeverBlocking(t *testing.T) {
	// Spec section 9: java is informational only. Missing, present, or
	// broken, it must never appear in a gate result.
	states := map[string]*Tool{
		"absent":  nil,
		"present": {Path: "/fake/java", OK: true},
		"broken":  {Path: "/broken/java", OK: false},
	}
	for name, java := range states {
		t.Run(name, func(t *testing.T) {
			env := completeEnv(t)
			env.Java = java

			res := env.Gate(Mode{Structure: true, Query: true, DocGen: true}, &config.Config{})

			for _, item := range res.Blocking {
				if strings.Contains(item.Item, "java") {
					t.Errorf("Blocking contains java item: %+v", item)
				}
			}
			for _, item := range res.Degrading {
				if strings.Contains(item.Item, "java") {
					t.Errorf("Degrading contains java item: %+v", item)
				}
			}
		})
	}
}
