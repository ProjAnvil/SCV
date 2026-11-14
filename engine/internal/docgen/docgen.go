// Package docgen implements doc generation (spec section 8) through two
// executor shapes behind one Generator: command mode — a configured
// external command (for example `claude -p`) invoked once per document
// with the document identity and context handed over via environment
// variables, never argv, so no per-run value can leak into process
// lists — and agent mode — an injected, already-initialized
// langchain-golang agent (anything satisfying AgentInvoker, e.g. the
// *agents.Agent returned by CreateAgent or CreateReactAgent) invoked
// once per document with the same assembled payload as the Human
// message. In both modes the executor's output text is the document;
// the engine writes it to the analysis dir, readme, summary,
// architecture, file_index in that order.
//
// Key custody: this package never reads, stores, or logs API keys.
// Whatever auth the command or the injected agent needs is the host's
// business — agent mode keeps model credentials entirely inside the
// host's own agent object.
//
// Generator implements run.DocGenerator, so the run pipeline accepts it
// directly. Progress granularity: every attempt emits a generating-stage
// sub-event (stage_started, with detail.doc and detail.attempt) through
// the caller's sink; a successful attempt closes it with stage_finished.
// The optional cooperating progress.jsonl protocol of spec section 8 is
// deliberately not implemented (YAGNI: engine tailing is a later task).
package docgen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	lcmessages "github.com/projanvil/langchain-golang/core/messages"
	"github.com/projanvil/scv/engine/internal/job"
)

// Pipeline error codes (spec sections 7 and 8).
const (
	CodeDocGenFailed = "DOCGEN_FAILED"
	CodeStageTimeout = "STAGE_TIMEOUT"
)

// DefaultTimeout bounds one command invocation when Timeout is zero or
// negative. Parked decision (Task 1 review, binding): a zero config
// timeout_s means "use the 600s default" — there is never a no-timeout
// mode.
const DefaultTimeout = 600 * time.Second

// logTailLimit caps the stderr tail carried in Failure.LogTail (spec
// section 7: <=2000 chars).
const logTailLimit = 2000

// waitDelay bounds cmd.Wait after a timeout kill, so a grandchild that
// inherited the pipes cannot outlive the report by holding them open.
const waitDelay = 2 * time.Second

// EnvAssets overrides asset resolution (tests, packed installs).
const EnvAssets = "SCV_DOCGEN_ASSETS"

// The exact environment contract handed to the command (spec section 8).
// Nothing else is added beyond the inherited environment, and argv never
// grows past the configured command.
const (
	EnvDoc           = "SCV_DOC"
	EnvRepository    = "SCV_REPOSITORY"
	EnvAnalysisDir   = "SCV_ANALYSIS_DIR"
	EnvStructureJSON = "SCV_STRUCTURE_JSON"
	EnvTemplate      = "SCV_TEMPLATE"
	EnvLang          = "SCV_LANG"
)

// docSlugs lists the four documents in generation order (spec section 8).
var docSlugs = []string{"readme", "summary", "architecture", "file_index"}

// canonicalNames maps doc slugs to their output file names.
var canonicalNames = map[string]string{
	"readme":       "README.md",
	"summary":      "SUMMARY.md",
	"architecture": "ARCHITECTURE.md",
	"file_index":   "FILE_INDEX.md",
}

// AgentInvoker is the executor seam of agent mode: the invoke surface
// of a langchain-golang agent. *agents.Agent (the return type of both
// agents.CreateAgent and langgraph prebuilt CreateReactAgent) satisfies
// it structurally, so hosts pass their initialized agent as-is. The
// skill payload travels as the Human message of each call — the agent's
// own system prompt, tools, and credentials stay untouched, which also
// means SCV never sees the model credentials.
//
// Agents built with a checkpointer share thread state across Invoke
// calls; hosts must pass a checkpoint-free agent (or isolate threads
// per call) so the four documents cannot pollute each other.
type AgentInvoker interface {
	Invoke(ctx context.Context, msgs []lcmessages.Message) ([]lcmessages.Message, error)
}

// Generator produces the four project documents by calling an external
// command once per document (spec section 8, command mode) or, when
// Invoker is set, an injected in-process agent once per document (agent
// mode). It implements run.DocGenerator.
type Generator struct {
	// Invoker selects agent mode: when non-nil the per-document payload
	// is delivered as one Human message to this agent instead of a
	// subprocess, and Command is ignored. Exactly one executor shape is
	// active per Generator; leaving both unset is a configuration error.
	Invoker AgentInvoker

	// Command is the LLM command, invoked exactly as configured — the
	// engine never appends arguments. Ignored in agent mode.
	Command []string

	// Timeout is the per-attempt wall-clock budget. Zero or negative
	// means DefaultTimeout; there is no no-timeout mode (parked decision:
	// config timeout_s == 0 means the 600s default).
	Timeout time.Duration

	// Retry is the number of ADDITIONAL attempts per document after the
	// first failure: config llm.retry == 1 means one retry, i.e. two
	// total attempts. Zero means a single attempt; negative is treated
	// as zero.
	Retry int

	// AssetsRoot is the packed docgen assets dir (<perception
	// root>/docgen/). Asset resolution order: the SCV_DOCGEN_ASSETS env
	// var (strict — a broken explicit override is an error), then this
	// field, then the repo-checkout fallback: the nearest ancestor of
	// AssetsRoot containing perception/ with agents/<lang>/ and
	// skills/<lang>/ (spec section 8's "a repo checkout works" clause).
	AssetsRoot string

	// Lang is the document language; "zh" maps to the zh-cn asset dir
	// (SCV_LANG carries the configured value, the mapping applies only
	// to asset lookup). Empty defaults to "zh".
	Lang string
}

// Failure is the typed error Generate returns when a document fails for
// good: the wire code (DOCGEN_FAILED or STAGE_TIMEOUT), the doc slug and
// attempt it died on, the human message, and the <=2000 char stderr
// tail. The run pipeline's fixed mapping reports every generator error
// as DOCGEN_FAILED with this error's text as the message, so Code and
// LogTail are carried on the struct for callers that map them onto
// job.EventError fields themselves.
type Failure struct {
	Code    string
	Doc     string
	Attempt int
	Message string
	LogTail string
}

// Error implements error.
func (f *Failure) Error() string {
	msg := fmt.Sprintf("docgen %s failed on attempt %d: %s", f.Doc, f.Attempt, f.Message)
	if f.LogTail != "" {
		msg += "\n" + f.LogTail
	}
	return msg
}

// Generate runs the command once per document in the frozen order
// readme→summary→architecture→file_index (spec section 8). Every attempt
// emits a generating-stage stage_started sub-event with detail.doc and
// detail.attempt through sink; a successful attempt emits stage_finished
// and its stdout is written to <analysisDir>/<canonical name>. On the
// first attempt that still fails after retries are exhausted, Generate
// returns a *Failure (exit != 0 or empty output ⇒ DOCGEN_FAILED with the
// stderr tail; timeout ⇒ STAGE_TIMEOUT after the process group is
// killed). Context cancellation returns early with the context error.
// The returned paths list is nil unless all four documents were written.
func (g *Generator) Generate(ctx context.Context, repoPath, analysisDir, structureJSONPath string, summary map[string]any, sink func(job.Event)) ([]string, error) {
	if g.Invoker == nil && len(g.Command) == 0 {
		return nil, errors.New("docgen: exactly one executor is required (config llm.command or an injected agent)")
	}
	if sink == nil {
		sink = func(job.Event) {}
	}
	lang := g.Lang
	if lang == "" {
		lang = "zh"
	}
	timeout := g.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	attempts := g.Retry + 1
	if g.Retry < 0 {
		attempts = 1
	}

	// Prompt assembly is a pure function over the asset files (spec
	// section 8): resolve and read everything before any command runs.
	assets, err := g.resolveAssets(langDir(lang))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(analysisDir, 0o755); err != nil {
		return nil, fmt.Errorf("docgen: create analysis dir: %w", err)
	}
	analyzerBody, err := os.ReadFile(assets.analyzer)
	if err != nil {
		return nil, fmt.Errorf("docgen: read project-analyzer: %w", err)
	}
	summaryText := renderSummary(summary)

	docs := make([]string, 0, len(docSlugs))
	for _, slug := range docSlugs {
		templatePath := assets.templates[slug]
		templateBody, err := os.ReadFile(templatePath)
		if err != nil {
			return nil, fmt.Errorf("docgen: read template %s: %w", templatePath, err)
		}
		task := docTask{
			slug:              slug,
			lang:              lang,
			templatePath:      templatePath,
			prompt:            buildPrompt(string(analyzerBody), summaryText, string(templateBody)),
			repoPath:          repoPath,
			analysisDir:       analysisDir,
			structureJSONPath: structureJSONPath,
			timeout:           timeout,
		}
		path, err := g.generateDoc(ctx, task, attempts, sink)
		if err != nil {
			return nil, err
		}
		docs = append(docs, path)
	}
	return docs, nil
}

// docTask carries one document's invocation inputs.
type docTask struct {
	slug              string
	lang              string
	templatePath      string
	prompt            string
	repoPath          string
	analysisDir       string
	structureJSONPath string
	timeout           time.Duration
}

// generateDoc drives the attempt loop for one document: emit the
// attempt's stage_started, run the command, and on success write the
// document and emit stage_finished. The last failing attempt returns the
// *Failure; earlier ones are retried without an intervening event.
func (g *Generator) generateDoc(ctx context.Context, task docTask, attempts int, sink func(job.Event)) (string, error) {
	outPath := filepath.Join(task.analysisDir, canonicalNames[task.slug])
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("docgen %s: %w", task.slug, err)
		}
		sink(docEvent(job.EventStageStarted, task.slug, attempt))

		var out []byte
		var failure *Failure
		var err error
		if g.Invoker != nil {
			out, failure, err = g.invokeAttempt(ctx, task, attempt)
		} else {
			out, failure, err = g.runAttempt(ctx, task, attempt)
		}
		if err != nil {
			return "", err // run cancelled, not a docgen failure
		}
		if failure == nil {
			if err := os.WriteFile(outPath, out, 0o644); err != nil {
				return "", fmt.Errorf("docgen %s: write %s: %w", task.slug, outPath, err)
			}
			sink(docEvent(job.EventStageFinished, task.slug, attempt))
			return outPath, nil
		}
		if attempt >= attempts {
			return "", failure
		}
	}
}

// runAttempt executes the command once for one attempt. Outcomes:
// success (doc bytes, nil, nil), a retryable *Failure (nil, failure,
// nil), or a run cancellation surfaced as a plain error (nil, nil, err).
// The child runs in its own process group; on timeout the whole group is
// SIGKILLed so grandchildren (the realistic shape of an LLM CLI) die too.
func (g *Generator) runAttempt(ctx context.Context, task docTask, attempt int) ([]byte, *Failure, error) {
	actx, cancel := context.WithTimeout(ctx, task.timeout)
	defer cancel()

	cmd := exec.CommandContext(actx, g.Command[0], g.Command[1:]...)
	cmd.Dir = task.repoPath
	cmd.Env = append(os.Environ(),
		EnvDoc+"="+task.slug,
		EnvRepository+"="+task.repoPath,
		EnvAnalysisDir+"="+task.analysisDir,
		EnvStructureJSON+"="+task.structureJSONPath,
		EnvTemplate+"="+task.templatePath,
		EnvLang+"="+task.lang,
	)
	cmd.Stdin = strings.NewReader(task.prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = waitDelay
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return nil, &Failure{
			Code:    CodeDocGenFailed,
			Doc:     task.slug,
			Attempt: attempt,
			Message: fmt.Sprintf("start command: %v", err),
		}, nil
	}
	if err := cmd.Wait(); err != nil {
		// The run itself being cancelled outranks the attempt outcome.
		if cerr := ctx.Err(); cerr != nil {
			return nil, nil, fmt.Errorf("docgen %s: %w", task.slug, cerr)
		}
		if actx.Err() != nil { // deadline hit; the group is already killed
			return nil, &Failure{
				Code:    CodeStageTimeout,
				Doc:     task.slug,
				Attempt: attempt,
				Message: fmt.Sprintf("timed out after %s", task.timeout),
			}, nil
		}
		return nil, &Failure{
			Code:    CodeDocGenFailed,
			Doc:     task.slug,
			Attempt: attempt,
			Message: fmt.Sprintf("command exited with error: %v", err),
			LogTail: clip(strings.TrimSpace(stderr.String()), logTailLimit),
		}, nil
	}
	if len(bytes.TrimSpace(stdout.Bytes())) == 0 {
		return nil, &Failure{
			Code:    CodeDocGenFailed,
			Doc:     task.slug,
			Attempt: attempt,
			Message: "command produced empty output",
		}, nil
	}
	return stdout.Bytes(), nil, nil
}

// invokeAttempt delivers one document's payload to the injected agent.
// Outcomes mirror runAttempt exactly: success (doc bytes, nil, nil), a
// retryable *Failure (nil, failure, nil), or a run cancellation
// surfaced as a plain error (nil, nil, err). The payload is the command
// mode's stdin prompt prefixed with an in-process equivalent of the
// environment contract header, delivered as a single Human message; the
// agent's own system prompt and tools stay as the host built them.
func (g *Generator) invokeAttempt(ctx context.Context, task docTask, attempt int) ([]byte, *Failure, error) {
	actx, cancel := context.WithTimeout(ctx, task.timeout)
	defer cancel()

	msgs := []lcmessages.Message{lcmessages.Human(taskHeader(task) + task.prompt)}
	res, err := g.Invoker.Invoke(actx, msgs)
	if err != nil {
		// The run itself being cancelled outranks the attempt outcome.
		if cerr := ctx.Err(); cerr != nil {
			return nil, nil, fmt.Errorf("docgen %s: %w", task.slug, cerr)
		}
		if actx.Err() != nil { // deadline hit before Invoke returned
			return nil, &Failure{
				Code:    CodeStageTimeout,
				Doc:     task.slug,
				Attempt: attempt,
				Message: fmt.Sprintf("agent timed out after %s: %v", task.timeout, err),
			}, nil
		}
		return nil, &Failure{
			Code:    CodeDocGenFailed,
			Doc:     task.slug,
			Attempt: attempt,
			Message: fmt.Sprintf("agent invoke: %v", err),
		}, nil
	}
	if cerr := ctx.Err(); cerr != nil { // Invoke returned, but the run is gone
		return nil, nil, fmt.Errorf("docgen %s: %w", task.slug, cerr)
	}
	if actx.Err() != nil {
		return nil, &Failure{
			Code:    CodeStageTimeout,
			Doc:     task.slug,
			Attempt: attempt,
			Message: fmt.Sprintf("agent timed out after %s", task.timeout),
		}, nil
	}
	text, ok := extractLastAI(res)
	if !ok {
		return nil, &Failure{
			Code:    CodeDocGenFailed,
			Doc:     task.slug,
			Attempt: attempt,
			Message: fmt.Sprintf("agent returned no assistant message (%d messages)", len(res)),
		}, nil
	}
	if strings.TrimSpace(text) == "" {
		return nil, &Failure{
			Code:    CodeDocGenFailed,
			Doc:     task.slug,
			Attempt: attempt,
			Message: "agent produced empty output",
		}, nil
	}
	return []byte(text), nil, nil
}

// taskHeader renders the in-process equivalent of the command mode's
// environment contract: the same document identity and context fields,
// as a compact block ahead of the prompt body.
func taskHeader(task docTask) string {
	var b strings.Builder
	b.WriteString("# SCV document task\n")
	fmt.Fprintf(&b, "doc: %s\n", task.slug)
	fmt.Fprintf(&b, "lang: %s\n", task.lang)
	fmt.Fprintf(&b, "repository: %s\n", task.repoPath)
	fmt.Fprintf(&b, "analysis_dir: %s\n", task.analysisDir)
	fmt.Fprintf(&b, "structure_json: %s\n", task.structureJSONPath)
	fmt.Fprintf(&b, "template: %s\n\n", task.templatePath)
	return b.String()
}

// extractLastAI returns the text of the last assistant message in the
// agent's reply, the in-process analogue of reading a command's stdout.
func extractLastAI(res []lcmessages.Message) (string, bool) {
	for i := len(res) - 1; i >= 0; i-- {
		if res[i].Role == lcmessages.RoleAI {
			return lcmessages.Text(res[i]), true
		}
	}
	return "", false
}

// docEvent builds one per-document generating-stage sub-event. ID/Seq/TS
// stay zero: the run pipeline forwards the event through the job log,
// which assigns them.
func docEvent(typ, slug string, attempt int) job.Event {
	return job.Event{
		Type:   typ,
		Stage:  string(job.StateGenerating),
		Detail: map[string]any{"doc": slug, "attempt": attempt},
	}
}

// buildPrompt assembles the stdin prompt (spec section 8): the
// project-analyzer instruction body, the structure summary rendering,
// and the template for this document, in that order.
func buildPrompt(analyzerBody, summaryText, templateBody string) string {
	var b strings.Builder
	b.WriteString(analyzerBody)
	b.WriteString("\n## Structure summary\n\n")
	b.WriteString(summaryText)
	b.WriteString("\n\n## Output template\n\n")
	b.WriteString(templateBody)
	b.WriteString("\n")
	return b.String()
}

// renderSummary renders the perceive stdout into prompt text: the
// pre-rendered structure_summary string when present, else the counts
// object, else the whole map as JSON, else an explicit placeholder.
func renderSummary(summary map[string]any) string {
	if len(summary) == 0 {
		return "(no structure summary)"
	}
	if s, ok := summary["structure_summary"].(string); ok && s != "" {
		return s
	}
	if c, ok := summary["counts"]; ok && c != nil {
		if data, err := json.MarshalIndent(c, "", "  "); err == nil {
			return string(data)
		}
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return "(unrenderable structure summary)"
	}
	return string(data)
}

// assetSet is the resolved prompt-asset layout for one language.
type assetSet struct {
	analyzer  string            // project-analyzer.md path
	templates map[string]string // doc slug → template file path
}

// templateName returns the template file name of a doc slug
// (README.md → README.template.md).
func templateName(slug string) string {
	return strings.TrimSuffix(canonicalNames[slug], ".md") + ".template.md"
}

// langDir maps a run language onto its asset directory (spec section 8:
// --lang zh uses the zh-cn assets).
func langDir(lang string) string {
	switch lang {
	case "", "zh":
		return "zh-cn"
	default:
		return lang
	}
}

// resolveAssets locates the prompt assets for the language directory
// dir, in the binding order: the SCV_DOCGEN_ASSETS env var (strict — a
// broken explicit override is an error, not a silent fallback), the
// AssetsRoot packed layout, then the repo-checkout fallback.
func (g *Generator) resolveAssets(dir string) (*assetSet, error) {
	if root := os.Getenv(EnvAssets); root != "" {
		return packedAssets(root, dir)
	}
	if g.AssetsRoot == "" {
		return nil, fmt.Errorf("docgen: no assets root: set %s or Generator.AssetsRoot", EnvAssets)
	}
	packed, packedErr := packedAssets(g.AssetsRoot, dir)
	if packedErr == nil {
		return packed, nil
	}
	if set, ok := checkoutAssets(g.AssetsRoot, dir); ok {
		return set, nil
	}
	return nil, fmt.Errorf("docgen: %v; repo-checkout fallback from %s found no agents/%s and skills assets",
		packedErr, g.AssetsRoot, dir)
}

// packedAssets resolves the packed runtime layout (make pack mirrors
// agents/ and skills/ into it): <root>/<dir>/project-analyzer.md and
// <root>/<dir>/<TEMPLATE>.template.md for every document.
func packedAssets(root, dir string) (*assetSet, error) {
	set := &assetSet{
		analyzer:  filepath.Join(root, dir, "project-analyzer.md"),
		templates: map[string]string{},
	}
	var missing []string
	if !fileExists(set.analyzer) {
		missing = append(missing, set.analyzer)
	}
	for _, slug := range docSlugs {
		p := filepath.Join(root, dir, templateName(slug))
		set.templates[slug] = p
		if !fileExists(p) {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("packed docgen assets missing: %s", strings.Join(missing, ", "))
	}
	return set, nil
}

// checkoutAssets implements the repo-checkout fallback: the nearest
// ancestor of anchor (inclusive) that contains perception/ and holds
// agents/<dir>/project-analyzer.md plus skills/<dir>/references/
// templates/<TEMPLATE>.template.md — the dev-machine layout when no
// runtime pack is installed.
func checkoutAssets(anchor, dir string) (*assetSet, bool) {
	abs, err := filepath.Abs(anchor)
	if err != nil {
		return nil, false
	}
	for root := abs; ; root = filepath.Dir(root) {
		if isDir(filepath.Join(root, "perception")) {
			set := &assetSet{
				analyzer:  filepath.Join(root, "agents", dir, "project-analyzer.md"),
				templates: map[string]string{},
			}
			ok := fileExists(set.analyzer)
			for _, slug := range docSlugs {
				p := filepath.Join(root, "skills", dir, "references", "templates", templateName(slug))
				set.templates[slug] = p
				ok = ok && fileExists(p)
			}
			if ok {
				return set, true
			}
		}
		parent := filepath.Dir(root)
		if parent == root {
			return nil, false
		}
	}
}

// clip truncates s to at most limit characters from the end (a tail).
func clip(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[len(s)-limit:]
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
