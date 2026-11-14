package main

// jobs.go implements the `scv jobs` verbs: list, status, events (--since),
// and cancel. Reads apply the spec section 7 crash rule — a dead-pid
// non-terminal job is marked failed/INTERRUPTED and the flip persisted —
// so every view reflects reality, not a pre-crash snapshot.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/projanvil/scv/engine/internal/job"
	"github.com/projanvil/scv/engine/internal/run"
)

func newJobsCmd() *cobra.Command {
	jobs := &cobra.Command{
		Use:   "jobs",
		Short: "Inspect and control recorded jobs",
	}
	jobs.AddCommand(newJobsListCmd(), newJobsStatusCmd(), newJobsEventsCmd(), newJobsCancelCmd())
	return jobs
}

// jobHome resolves <scv-home>/jobs/<id>, rejecting ids that would escape
// the jobs directory.
func jobHome(id string) (string, error) {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return "", fmt.Errorf("invalid job id %q", id)
	}
	home, err := opts.home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "jobs", id), nil
}

// loadJobForRead loads a job for any `scv jobs` read path and applies the
// crash rule of spec section 7: a non-terminal job whose owning process
// is dead is marked failed/INTERRUPTED and the snapshot is persisted, so
// a host that died mid-run reports honestly on the next read.
func loadJobForRead(dir string) (*job.Job, []job.Event, error) {
	j, events, err := job.LoadStatus(dir)
	if err != nil {
		return nil, nil, err
	}
	if job.MarkInterruptedIfDead(j) {
		if err := j.Snapshot(dir); err != nil {
			return nil, nil, fmt.Errorf("persist INTERRUPTED state: %w", err)
		}
	}
	return j, events, nil
}

// loadJobForCommand is loadJobForRead mapped onto the CLI exit codes:
// a missing job directory is NOT_FOUND, anything else a generic failure.
func loadJobForCommand(id string) (string, *job.Job, []job.Event, error) {
	dir, err := jobHome(id)
	if err != nil {
		return "", nil, nil, fail(exitUsage, "%v", err)
	}
	j, events, err := loadJobForRead(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil, nil, fail(exitNotFound, "no job %s", id)
		}
		return "", nil, nil, fail(exitError, "read job %s: %v", id, err)
	}
	return dir, j, events, nil
}

func newJobsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List recorded jobs, newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := opts.home()
			if err != nil {
				return fail(exitError, "%v", err)
			}
			dirEntries, err := os.ReadDir(filepath.Join(home, "jobs"))
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					dirEntries = nil // fresh install: no jobs yet
				} else {
					return fail(exitError, "list jobs: %v", err)
				}
			}
			var jobs []*job.Job
			for _, de := range dirEntries {
				if !de.IsDir() {
					continue
				}
				j, _, err := loadJobForRead(filepath.Join(home, "jobs", de.Name()))
				if err != nil {
					diagf("skipping unreadable job %s: %v", de.Name(), err)
					continue
				}
				jobs = append(jobs, j)
			}
			sort.Slice(jobs, func(a, b int) bool {
				return jobs[a].StartedAt.After(jobs[b].StartedAt)
			})
			if opts.jsonOut {
				return emitJSON(jobs)
			}
			for _, j := range jobs {
				fmt.Printf("%s  %-10s  %-12s  %s\n", j.ID, j.State, j.Stage, j.StartedAt.Format(timeFormat))
			}
			return nil
		},
	}
}

// timeFormat is the human timestamp format of the jobs text output.
const timeFormat = "2006-01-02T15:04:05Z07:00"

func newJobsStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <id>",
		Short: "Show one job's current snapshot (job.json)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, j, events, err := loadJobForCommand(args[0])
			if err != nil {
				return err
			}
			if opts.jsonOut {
				return emitJSON(j)
			}
			fmt.Printf("job:     %s\n", j.ID)
			fmt.Printf("state:   %s\n", j.State)
			fmt.Printf("stage:   %s\n", j.Stage)
			fmt.Printf("pid:     %d\n", j.Pid)
			fmt.Printf("started: %s\n", j.StartedAt.Format(timeFormat))
			fmt.Printf("updated: %s\n", j.UpdatedAt.Format(timeFormat))
			fmt.Printf("events:  %d\n", len(events))
			if code := errorCodeOf(j.Detail); code != "" {
				fmt.Printf("error:   %s\n", code)
			}
			return nil
		},
	}
}

// errorCodeOf extracts the error code a job snapshot carries in its
// detail (set by the failed/cancelled terminal transitions). The
// in-memory value after a JSON round trip is a map; both shapes are
// handled.
func errorCodeOf(detail map[string]any) string {
	if detail == nil {
		return ""
	}
	switch e := detail["error"].(type) {
	case map[string]any:
		code, _ := e["code"].(string)
		return code
	case job.EventError:
		return e.Code
	}
	return ""
}

func newJobsEventsCmd() *cobra.Command {
	var since int
	cmd := &cobra.Command{
		Use:   "events <id>",
		Short: "Print a job's event log as NDJSON",
		Long: "Print the job's events.jsonl as NDJSON, one event per line. --since SEQ\n" +
			"returns only events with a sequence number strictly greater than SEQ\n" +
			"(the tail-call form: resume from the last seq already seen).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, events, err := loadJobForCommand(args[0])
			if err != nil {
				return err
			}
			printer := &eventPrinter{w: os.Stdout}
			for _, ev := range events {
				if ev.Seq > since {
					printer.print(ev)
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&since, "since", 0, "print only events with seq greater than N")
	return cmd
}

func newJobsCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <id>",
		Short: "Cancel a running job (SIGTERM its process group, then move it to cancelled)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			dir, j, _, err := loadJobForCommand(id)
			if err != nil {
				return err
			}
			if err := run.Cancel(dir, j); err != nil {
				if errors.Is(err, run.ErrWorkflowNotCancellable) {
					return fail(exitNotCancellable, "%v", err)
				}
				return fail(exitError, "cancel job %s: %v", id, err)
			}
			if opts.jsonOut {
				return emitJSON(map[string]any{"id": id, "cancelled": true})
			}
			fmt.Printf("job %s cancelled\n", id)
			return nil
		},
	}
}
