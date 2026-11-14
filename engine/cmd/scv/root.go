package main

// root.go holds the scv root command, the global flags, and the shared
// plumbing every subcommand uses: exit-code mapping, JSON emission, and
// human diagnostics.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// Exit codes. 0 is success — including degraded-but-done and skipped
// runs; every other code is a failure. Typed errors map as follows:
//
//	0   success
//	1   generic failure (CLONE_FAILED, PULL_FAILED, ANALYZER_FAILED,
//	    DOCGEN_FAILED, FINALIZE_FAILED, query/store errors, I/O errors)
//	2   usage error (bad flags or arguments; cobra's own failures)
//	3   VERSION_INCOMPATIBLE  — run refused by the version handshake
//	4   WORKFLOW_NOT_CANCELLABLE — `jobs cancel` on an ended job
//	5   WORKFLOW_LOCKED        — run refused by the per-analysis-dir lock
//	6   NOT_FOUND              — unknown job id or repos entry
//	7   NAME_EXISTS            — repo clone/link name collision
//	8   PATH_NOT_FOUND         — missing run source / repo link path
//	9   ENV_INCOMPLETE         — blocking gate items at checking
//	10  CANCELLED              — the run ended cancelled
const (
	exitOK                  = 0
	exitError               = 1
	exitUsage               = 2
	exitVersionIncompatible = 3
	exitNotCancellable      = 4
	exitWorkflowLocked      = 5
	exitNotFound            = 6
	exitNameExists          = 7
	exitPathNotFound        = 8
	exitEnvIncomplete       = 9
	exitCancelled           = 10
)

// Engine identity and the perceive contract range it supports (spec
// section 12). contractMin/contractMax are compared by major version
// only: additive minor evolution never breaks the handshake.
const (
	contractMin = "1.0"
	contractMax = "1.x"
)

// engineVersion is the engine identity reported by `scv --version`.
// Release binaries are stamped at pack time: scripts/pack.sh builds with
// -ldflags "-X main.engineVersion=<ver>", taking <ver> from the first
// released "## [vX.Y.Z]" heading in CHANGELOG.md — the same version the
// artifact names carry. Unstamped builds (go build in a checkout, test
// binaries) report the "dev" fallback below.
var engineVersion = "dev"

// versionJSON renders the documented `scv --version` output: one JSON
// object with the engine version and the supported perceive contract
// range. A function, not a constant, because engineVersion is a
// link-time variable.
func versionJSON() string {
	return fmt.Sprintf(`{"engine":%q,"contract_min":%q,"contract_max":%q}`,
		engineVersion, contractMin, contractMax)
}

// options carries the global flags shared by every subcommand.
type options struct {
	scvHome string
	config  string
	jsonOut bool
	quiet   bool
}

// opts is bound by the root command's persistent flags. It is CLI wiring
// state, populated once during flag parsing before any command runs.
var opts options

// codedError couples an error message with the process exit code, so a
// RunE failure maps straight onto the documented exit-code table (the
// exitError constant above is the code-1 entry of that table; this type
// is what carries any of the codes).
type codedError struct {
	code int
	msg  string
}

func (e *codedError) Error() string { return e.msg }

// fail builds the error a RunE returns on failure: cobra prints msg to
// stderr and main turns code into the process exit status.
func fail(code int, format string, args ...any) error {
	return &codedError{code: code, msg: fmt.Sprintf(format, args...)}
}

// home resolves the scv home: the --scv-home flag, then $SCV_HOME, then
// ~/.scv (spec section 9's resolution order).
func (o *options) home() (string, error) {
	if o.scvHome != "" {
		return o.scvHome, nil
	}
	if v := os.Getenv("SCV_HOME"); v != "" {
		return v, nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(userHome, ".scv"), nil
}

// configPath resolves the config file: the --config flag, else
// <scv-home>/config.json.
func (o *options) configPath() (string, error) {
	if o.config != "" {
		return o.config, nil
	}
	home, err := o.home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "config.json"), nil
}

// diagf prints one human diagnostic line to stderr unless --quiet. The
// `scv run` stdout is reserved for the NDJSON event stream; every human
// message goes through here.
func diagf(format string, args ...any) {
	if !opts.quiet {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

// emitJSON prints v as one compact JSON value on stdout — the --json
// contract of the synchronous verbs: exactly one JSON value, one line.
// A render failure is returned as a coded error (generic exit 1), never
// a raw error that main would misread as a usage failure.
func emitJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fail(exitError, "render JSON output: %v", err)
	}
	fmt.Println(string(data))
	return nil
}

// newRootCmd assembles the scv command tree with its global flags.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "scv",
		Short:        "SCV engine — Go control plane over the perception toolchain",
		Version:      versionJSON(),
		SilenceUsage: true, // runtime failures are not usage errors
	}
	root.SetVersionTemplate(versionJSON() + "\n")

	pf := root.PersistentFlags()
	pf.StringVar(&opts.scvHome, "scv-home", "", "scv home directory (default $SCV_HOME, else ~/.scv)")
	pf.StringVar(&opts.config, "config", "", "config file (default <scv-home>/config.json)")
	pf.BoolVar(&opts.jsonOut, "json", false, "machine-readable JSON output for the synchronous verbs")
	pf.BoolVar(&opts.quiet, "quiet", false, "suppress human diagnostics on stderr")

	root.AddCommand(
		newEnvCmd(),
		newRunCmd(),
		newRepoCmd(),
		newJobsCmd(),
		newQueryCmd(),
	)
	return root
}
