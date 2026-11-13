package run

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/projanvil/scv/engine/internal/config"
	"github.com/projanvil/scv/engine/internal/env"
	"github.com/projanvil/scv/engine/internal/job"
	"github.com/projanvil/scv/engine/internal/repo"
)

// logTailLimit is the error.log_tail cap (spec section 7: <=2000 chars).
const logTailLimit = 2000

// tailPollInterval is how often the perception.log tailer polls the file
// for new tier lines while perceive.py runs.
const tailPollInterval = 200 * time.Millisecond

// testPerceiveEnv names the perceive path override. It exists so engine
// unit tests (this package, T10, T12) can point the pipeline at a stub
// perceive script instead of the real runtime — see perceivePath.
const testPerceiveEnv = "SCV_ENGINE_TEST_PERCEIVE"

// perceivePath resolves the perceive script to execute: the
// SCV_ENGINE_TEST_PERCEIVE env var when set (a test seam that replaces
// the real script with a stub; unset in production), else
// <perception root>/perceive.py.
func perceivePath(e *env.Env) string {
	if p := os.Getenv(testPerceiveEnv); p != "" {
		return p
	}
	return filepath.Join(e.PerceptionRoot, "perceive.py")
}

// sourceFlag maps the source kind onto perceive.py's --source values.
// perceive accepts only local|remote (perception/perceive.py argparse),
// so both local kinds share "local" and only URLs run remote.
func sourceFlag(kind repo.SourceKind) string {
	if kind == repo.KindURL {
		return "remote"
	}
	return "local"
}

// ParsePerceiveJSON parses one line of perceive stdout into a generic
// map. It is lenient by contract: unknown fields are kept, only a line
// that is not a JSON object is an error.
func ParsePerceiveJSON(line string) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return nil, fmt.Errorf("parse perceive stdout JSON: %w", err)
	}
	return m, nil
}

// runPerceiveStage executes perceive.py and maps its perception.log tier
// lines to stage events live (spec section 7): tier1→detecting,
// tier2|tier3→analyzing, tier6→grading, tier8→query_tier. Stages whose
// capability the gate degraded are not mapped (structure off suppresses
// analyzing; query off suppresses grading/query_tier), which is what
// produces the reduced frozen sequences. Runtime failures map to
// stage_failed ANALYZER_FAILED with a <=2000 char log tail; context
// cancellation terminates the job as cancelled.
func runPerceiveStage(ctx context.Context, em *emitter, cfg *config.Config, e *env.Env, p *prepared, req RunRequest, structureOn, queryOn bool) (map[string]any, error) {
	script := perceivePath(e)
	if !fileExists(script) {
		return nil, analyzerFailure(em, nil, "",
			fmt.Sprintf("perceive script not found: %s", script))
	}
	if err := os.MkdirAll(p.analysisDir, 0o755); err != nil {
		return nil, analyzerFailure(em, nil, "", fmt.Sprintf("create analysis dir: %v", err))
	}
	logPath := filepath.Join(p.analysisDir, "perception.log")
	offset := fileSize(logPath) // only lines appended by this run count
	tracker := &tierTracker{em: em, analysisDir: p.analysisDir, structureOn: structureOn, queryOn: queryOn}

	lang := req.Lang
	if lang == "" {
		lang = "zh" // perceive.py's own default
	}
	cmd := exec.CommandContext(ctx, e.Python3.Path, script, p.repoPath, p.analysisDir,
		"--source", sourceFlag(p.kind), "--lang", lang)
	// perceive defaults its config to ~/.scv/config.json; the engine must
	// point it at the config this run loaded, which can diverge under
	// --config/--scv-home overrides.
	cmd.Env = append(os.Environ(), "SCV_PERCEPTION_CONFIG="+cfg.Path)
	cmd.Dir = p.repoPath
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, analyzerFailure(em, tracker, "", fmt.Sprintf("start perceive: %v", err))
	}
	waitErr := func() error {
		var waitErr error
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		ticker := time.NewTicker(tailPollInterval)
		defer ticker.Stop()
		for waiting := true; waiting; {
			select {
			case waitErr = <-done:
				waiting = false
			case <-ticker.C:
			case <-ctx.Done():
				// CommandContext is already killing the child; keep
				// draining until Wait reports it gone.
			}
			tracker.drain(logPath, &offset)
		}
		return waitErr
	}()

	if ctx.Err() != nil {
		if tracker.open != "" {
			if err := em.emit(job.EventStageFailed, tracker.open, nil, &job.EventError{
				Code:      CodeCancelled,
				Message:   "perceive cancelled before the stage finished",
				Retryable: true,
			}); err != nil {
				return nil, err
			}
			tracker.open = ""
		}
		if err := em.terminate(job.StateCancelled, nil, &job.EventError{
			Code:      CodeCancelled,
			Message:   fmt.Sprintf("run cancelled: %v", ctx.Err()),
			Retryable: true,
		}); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("run cancelled: %w", ctx.Err())
	}

	tail := logTail(logPath, stdout.String())
	if waitErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		return nil, analyzerFailure(em, tracker, tail,
			fmt.Sprintf("perceive exited with error: %v: %s", waitErr, clip(msg, logTailLimit)))
	}

	line := lastNonEmptyLine(stdout.String())
	perception, parseErr := ParsePerceiveJSON(line)
	if parseErr != nil {
		return nil, analyzerFailure(em, tracker, tail,
			fmt.Sprintf("perceive stdout is not JSON (%q)", clip(line, logTailLimit)))
	}
	// In-band runtime failure (exit-0 contract): the analyzer tier
	// reported itself failed; the open analyzing stage fails.
	if status, _ := perception["status"].(string); status == "skipped" {
		if reason, _ := perception["reason"].(string); reason == "analyzer-failed" {
			return nil, analyzerFailure(em, tracker, tail, "perceive reported analyzer-failed")
		}
	}
	tracker.finishOpen()
	return perception, nil
}

// analyzerFailure emits the stage_failed ANALYZER_FAILED event and the
// failed job_result, targeting the stage the tracker had open (else the
// last stage seen, else detecting — the perceive entry stage), and
// returns the sentinel-wrapped error.
func analyzerFailure(em *emitter, t *tierTracker, logTailText, msg string) error {
	stage := string(job.StateDetecting)
	if t != nil {
		if t.open != "" {
			stage = t.open
		} else if t.last != "" {
			stage = t.last
		}
		t.open = ""
	}
	return em.fail(job.State(stage), job.EventError{
		Code:      CodeAnalyzerFailed,
		Message:   msg,
		Retryable: true,
		LogTail:   logTailText,
	}, nil, fmt.Errorf("%w: %s", ErrAnalyzerFailed, msg))
}

// tierTracker maps perception.log tier lines onto pipeline stage events.
// A tier's "start" line opens its stage (closing any open stage first —
// a transition proves the previous tier finished); completion lines
// either close the stage (tier1 detect) or accumulate detail (tier2 uv
// probe, tier3 analyzer rc) for the close that the next transition or
// process exit performs. Stages whose capability was degraded at the
// gate are never opened, whatever the log says.
type tierTracker struct {
	em          *emitter
	analysisDir string
	structureOn bool
	queryOn     bool

	open   string         // currently open stage, "" when none
	detail map[string]any // detail accumulated for the open stage
	last   string         // last stage seen, for failure attribution
}

// tierStages maps perceive tier numbers to pipeline stages (spec §7).
var tierStages = map[int]string{
	1: string(job.StateDetecting),
	2: string(job.StateAnalyzing),
	3: string(job.StateAnalyzing),
	6: string(job.StateGrading),
	8: string(job.StateQueryTier),
}

// tierLine is one parsed perception.log line relevant to the tracker.
type tierLine struct {
	tier int    // 0 for the "tiers done" sentinel
	rest string // text after "tierN "
	done bool   // the final "tiers done" line
}

// parseTierLine recognizes the tier lines perceive.py writes, with or
// without the leading timestamp token. Anything else is not a tier line.
func parseTierLine(s string) (tierLine, bool) {
	body := s
	if !strings.HasPrefix(body, "tier") {
		if i := strings.IndexByte(body, ' '); i >= 0 && strings.HasPrefix(body[i+1:], "tier") {
			body = body[i+1:]
		} else {
			return tierLine{}, false
		}
	}
	if strings.HasPrefix(body, "tiers done") {
		return tierLine{done: true}, true
	}
	num, rest, ok := strings.Cut(strings.TrimPrefix(body, "tier"), " ")
	if !ok {
		return tierLine{}, false
	}
	n, err := strconv.Atoi(num)
	if err != nil {
		return tierLine{}, false
	}
	return tierLine{tier: n, rest: rest}, true
}

// drain reads newly appended complete lines from path and feeds them to
// the tracker, advancing *offset. perceive.py's cleanup_stale deletes
// and recreates perception.log at run start, so a file smaller than the
// recorded offset is a recreation: restart from the top.
func (t *tierTracker) drain(path string, offset *int64) {
	data, err := os.ReadFile(path)
	if err != nil {
		return // no log yet — nothing to tail
	}
	if int64(len(data)) < *offset {
		*offset = 0
	}
	if int64(len(data)) <= *offset {
		return
	}
	chunk := data[*offset:]
	end := bytes.LastIndexByte(chunk, '\n')
	if end < 0 {
		return // no complete new line yet
	}
	for _, line := range strings.Split(string(chunk[:end]), "\n") {
		t.line(line)
	}
	*offset += int64(end) + 1
}

// line applies one perception.log line to the stage machine.
func (t *tierTracker) line(s string) {
	tl, ok := parseTierLine(strings.TrimRight(s, "\r"))
	if !ok {
		return
	}
	if tl.done {
		t.finishOpen() // everything after "tiers done" is teardown
		return
	}
	switch {
	case tl.rest == "start":
		t.start(tierStages[tl.tier])
	case tl.tier == 1 && strings.HasPrefix(tl.rest, "detect:"):
		t.detectFinish(firstField(tl.rest[len("detect:"):]))
	case tl.tier == 2 && strings.HasPrefix(tl.rest, "uv probe:"):
		t.note(string(job.StateAnalyzing), "uv", strings.TrimSpace(tl.rest[len("uv probe:"):]))
	case tl.tier == 3 && strings.HasPrefix(tl.rest, "analyzer rc="):
		t.note(string(job.StateAnalyzing), "analyzer_rc", firstToken(tl.rest[len("analyzer rc="):]))
	}
}

// start opens the stage for a tier start line. Suppressed stages are
// ignored; a start of the already-open stage (tier3 while analyzing) is
// a no-op; otherwise the open stage closes as finished first.
func (t *tierTracker) start(stage string) {
	if stage == "" || t.suppressed(stage) {
		return
	}
	t.last = stage
	if stage == t.open {
		return
	}
	t.finishOpen()
	t.open, t.detail = stage, map[string]any{}
	_ = t.em.enterStage(job.State(stage))
}

// detectFinish closes detecting with the detect verdict. A detect
// completion with no open stage is the not-framework path (perceive logs
// only "tier1 detect: not-framework"): the stage opens implicitly so the
// frozen sequence still shows detecting.
func (t *tierTracker) detectFinish(status string) {
	detail := map[string]any{"perception_status": status}
	if t.open == string(job.StateDetecting) {
		t.last = t.open
		t.open = ""
		_ = t.em.finishStage(job.StateDetecting, detail)
		return
	}
	if t.open == "" {
		t.last = string(job.StateDetecting)
		_ = t.em.enterStage(job.StateDetecting)
		_ = t.em.finishStage(job.StateDetecting, detail)
	}
}

// note records tier completion detail for the open stage's finish event.
func (t *tierTracker) note(stage, key, value string) {
	if t.open == stage && t.detail != nil {
		t.detail[key] = value
	}
}

// finishOpen closes the open stage as finished, applying the
// belt-and-braces artifact check for the loader tier (structure.vg
// existence — tier6 logs nothing on success) to the finish detail.
func (t *tierTracker) finishOpen() {
	if t.open == "" {
		return
	}
	detail := t.detail
	if detail == nil {
		detail = map[string]any{}
	}
	if t.open == string(job.StateGrading) {
		detail["structure_vg"] = fileExists(filepath.Join(t.analysisDir, "structure.vg"))
	}
	stage := t.open
	t.last, t.open, t.detail = stage, "", nil
	_ = t.em.finishStage(job.State(stage), detail)
}

// suppressed reports whether the gate degraded the capability a stage
// belongs to: analyzing needs structure, grading/query_tier need query.
func (t *tierTracker) suppressed(stage string) bool {
	switch stage {
	case string(job.StateAnalyzing):
		return !t.structureOn
	case string(job.StateGrading), string(job.StateQueryTier):
		return !t.queryOn
	default:
		return false
	}
}

// logTail returns the last logTailLimit characters of the perception
// log; when the log is missing or empty (perceive died before writing
// anything), the captured stdout stands in as the diagnostic.
func logTail(path, stdout string) string {
	data, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		data = []byte(stdout)
	}
	return clip(string(data), logTailLimit)
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// lastNonEmptyLine returns the final non-empty line of s — perceive
// prints its JSON result as the last stdout line.
func lastNonEmptyLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

// clip truncates s to at most limit characters from the end (a log tail).
func clip(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[len(s)-limit:]
}

// firstField returns the first comma-separated field, trimmed.
func firstField(s string) string {
	f, _, _ := strings.Cut(s, ",")
	return strings.TrimSpace(f)
}

// firstToken returns the first space-separated token, trimmed.
func firstToken(s string) string {
	f, _, _ := strings.Cut(s, " ")
	return strings.TrimSpace(f)
}
