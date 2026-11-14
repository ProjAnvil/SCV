package job

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Event types (spec section 7). The strings are the exact wire format
// shared by events.jsonl and the NDJSON stdout of `scv run`, so they
// must never be renamed.
const (
	EventStageStarted     = "stage_started"
	EventStageFinished    = "stage_finished"
	EventStageFailed      = "stage_failed"
	EventDegraded         = "degraded"
	EventArtifactProduced = "artifact_produced"
	EventJobResult        = "job_result"
)

// EventError is the error payload of stage_failed and job_result
// events (spec section 7): an uppercase code, a human message, whether
// a re-run can help, and the <=2000 char tail of the failing process
// log.
type EventError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	LogTail   string `json:"log_tail"`
}

// Event is one line of the append-only events.jsonl log. Seq is
// monotonic per job, starting at 1; `scv jobs events --since SEQ`
// relies on that ordering.
type Event struct {
	ID     string         `json:"id"`
	Seq    int            `json:"seq"`
	TS     time.Time      `json:"ts"`
	Type   string         `json:"type"`
	Stage  string         `json:"stage"`
	Detail map[string]any `json:"detail,omitempty"`
	Error  *EventError    `json:"error,omitempty"`
}

// Emit appends one event to <dir>/events.jsonl and returns it. The
// sequence number continues from the last event the job emitted or, for
// a job restored by LoadStatus, from the last event on disk, so seq is
// monotonic per job. Every append is fsynced, and a failed append does
// not consume a sequence number. Emit is single-writer: one owning
// process appends to a job's log; readers use LoadStatus.
func (j *Job) Emit(dir string, typ, stage string, detail map[string]any, evErr *EventError) (Event, error) {
	ev := Event{
		ID:     j.ID,
		Seq:    j.lastSeq + 1,
		TS:     time.Now().UTC(),
		Type:   typ,
		Stage:  stage,
		Detail: detail,
		Error:  evErr,
	}
	if err := appendEvent(filepath.Join(dir, eventsFile), ev); err != nil {
		return Event{}, err
	}
	j.lastSeq = ev.Seq
	return ev, nil
}

// appendEvent marshals ev to one JSON line and appends it (newline
// terminated, fsynced) to the log at path, creating the log on first
// use.
func appendEvent(path string, ev Event) error {
	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open events log: %w", err)
	}
	// Unchecked deferred close: writeSynced has already fsynced, so the
	// append is durable no matter what Close reports.
	defer f.Close()
	return writeSynced(f, append(line, '\n'))
}

// syncWriter is the writable, fsyncable surface of *os.File, injected
// in tests to force write and sync failures.
type syncWriter interface {
	Write(p []byte) (int, error)
	Sync() error
}

// writeSynced writes data to w and fsyncs it, so a crash right after a
// successful call cannot lose the write.
func writeSynced(w syncWriter, data []byte) error {
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := w.Sync(); err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	return nil
}

// fileLike is the surface of the tmp *os.File that writeSnapshot needs.
type fileLike interface {
	syncWriter
	Close() error
	Name() string
}

// writeSnapshot writes data to tmp, fsyncs it, closes it, and renames
// it over target — the atomic swap behind Job.Snapshot. On any failure
// the tmp file is removed; after a successful rename the (already
// moved) file is never deleted even if Close reports a late error.
func writeSnapshot(tmp fileLike, data []byte, target string) (err error) {
	renamed := false
	defer func() {
		if cerr := tmp.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close snapshot tmp file: %w", cerr)
		}
		if err != nil && !renamed {
			os.Remove(tmp.Name())
		}
	}()
	if err := writeSynced(tmp, data); err != nil {
		return fmt.Errorf("write snapshot tmp file: %w", err)
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("swap snapshot into place: %w", err)
	}
	renamed = true
	return nil
}

// readEvents loads the event log at path. A missing log is an empty
// log (a job that has not emitted yet), not an error.
func readEvents(path string) ([]Event, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read events log: %w", err)
	}
	return parseEvents(data)
}

// parseEvents parses raw events.jsonl content. A crash mid-append can
// tear only the final line, so content that does not end in a newline
// has its trailing partial line dropped without an error; a complete
// line that fails to parse is real corruption and an error.
func parseEvents(data []byte) ([]Event, error) {
	// A successful append always ends with '\n' (the last byte written),
	// so an unterminated tail is exactly a torn append.
	if len(data) > 0 && data[len(data)-1] != '\n' {
		if end := bytes.LastIndexByte(data, '\n'); end >= 0 {
			data = data[:end+1]
		} else {
			data = nil
		}
	}
	var events []Event
	for i, line := range bytes.Split(data, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, fmt.Errorf("parse event line %d: %w", i+1, err)
		}
		events = append(events, ev)
	}
	return events, nil
}
