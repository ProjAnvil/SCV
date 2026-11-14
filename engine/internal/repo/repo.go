// Package repo manages the repository entries under an SCV home
// (<scvHome>/repos): cloning remote git URLs, linking local git
// repositories as symlinks, pulling updates, listing entries, and
// removing them (spec section 6; a port of skills/scripts/git_op.py
// semantics — clone defaults to branch main, entry names derive from
// the last path segment with ".git" stripped).
//
// Entry kinds:
//
//   - remote — a git clone owned by SCV; Pull applies
//   - linked — an absolute symlink to a local worktree; analysis reads
//     the worktree as-is (uncommitted changes included), and Remove
//     never touches the target directory
//   - dir    — anything else found under repos/ (no symlink, no origin)
//
// Errors carry pipeline error codes as sentinel-wrapped prefixes
// (CLONE_FAILED, PATH_NOT_FOUND, ...): the run pipeline maps them to
// stage_failed events and the CLI maps them to exit codes and messages.
package repo

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultBranch is the branch Clone uses when none is requested — the
// git_op.py default.
const DefaultBranch = "main"

// Entry directory names inside the SCV home.
const (
	reposDirName    = "repos"
	analysisDirName = "analysis"
)

// Entry type values: the exact wire strings of `scv repo list` output.
const (
	TypeRemote = "remote"
	TypeLinked = "linked"
	TypeDir    = "dir"
)

// Sentinel error codes. ErrCloneFailed and ErrPathNotFound are the two
// the run pipeline maps during its preparing stage; the rest drive CLI
// exit codes and messages.
var (
	ErrCloneFailed  = errors.New("CLONE_FAILED")
	ErrPathNotFound = errors.New("PATH_NOT_FOUND")
	ErrPullFailed   = errors.New("PULL_FAILED")
	ErrNameExists   = errors.New("NAME_EXISTS")
	ErrNotFound     = errors.New("NOT_FOUND")
)

// Entry is one repository entry under <scvHome>/repos, as produced by
// Clone, Link, and Pull and listed by List. Path is the entry's own
// location (<scvHome>/repos/<name>); for linked entries it resolves
// through the symlink to the target worktree. URL, Branch, and
// HeadCommit are best-effort metadata and stay empty when the
// underlying git query has no answer (no remote, non-git directory).
type Entry struct {
	Name       string
	Path       string
	URL        string
	Branch     string
	Type       string
	HeadCommit string
}

// SourceKind is how Classify routes a run source through the pipeline
// (spec section 6): URL sources are cloned, local git repositories are
// linked, local directories are analyzed in place.
type SourceKind string

// Source kinds. KindURL maps to perceive's `--source remote`; the two
// local kinds map to `--source local`.
const (
	KindURL      SourceKind = "url"
	KindLocalGit SourceKind = "local_git"
	KindLocalDir SourceKind = "local_dir"
)

// Classify decides how the run pipeline must handle source (spec
// section 6): a git URL is cloned (KindURL), an existing local git
// repository is linked (KindLocalGit), and anything else is a local
// directory analyzed in place (KindLocalDir). A source that is neither
// a URL nor an existing git repository classifies as KindLocalDir even
// when it does not exist: classification is pure, and the missing path
// surfaces later as PATH_NOT_FOUND where the pipeline maps it.
func Classify(source string) SourceKind {
	if isGitURL(source) {
		return KindURL
	}
	if isGitRepo(source) {
		return KindLocalGit
	}
	return KindLocalDir
}

// scpLikeURL matches the scp-style git URL form user@host:path; the
// colon after the host is what distinguishes it from a local path.
var scpLikeURL = regexp.MustCompile(`^[A-Za-z0-9._~+-]+@[A-Za-z0-9.-]+:`)

// isGitURL reports whether source carries a URL scheme (https://,
// git://, ssh://, ...) or matches the scp-style user@host:path form.
func isGitURL(source string) bool {
	if strings.Contains(source, "://") {
		return true
	}
	return scpLikeURL.MatchString(source)
}

// Clone clones url into <scvHome>/repos/<name> on branch branch (empty
// means DefaultBranch) and returns the new remote Entry. The name
// follows git_op.py extract_repo_name: the url's last path segment,
// trailing slashes dropped, ".git" suffix stripped. An existing entry
// with that name is refused (ErrNameExists) — nothing is ever
// clobbered — and a git failure is wrapped with ErrCloneFailed.
func Clone(scvHome, url, branch string) (*Entry, error) {
	if branch == "" {
		branch = DefaultBranch
	}
	name := repoNameFromURL(url)
	if err := validName(name); err != nil {
		return nil, fmt.Errorf("%w: derive repository name from %q: %v", ErrCloneFailed, url, err)
	}
	reposDir := filepath.Join(scvHome, reposDirName)
	target := filepath.Join(reposDir, name)
	if err := checkVacant(target); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(reposDir, 0o755); err != nil {
		return nil, fmt.Errorf("repo clone: create %s: %w", reposDir, err)
	}
	if _, err := runGit("", "clone", "-b", branch, url, target); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCloneFailed, err)
	}
	e := &Entry{Name: name, Path: target, URL: url, Branch: branch, Type: TypeRemote}
	// Best effort: a repository with no commits yet has no HEAD to name.
	e.HeadCommit, _ = HeadCommit(target)
	return e, nil
}

// Link creates the entry <scvHome>/repos/<name> as an absolute symlink
// to path and returns the linked Entry. An empty name defaults to the
// base name of the resolved path. A missing or non-directory path is
// ErrPathNotFound; an existing entry with the same name is refused
// (ErrNameExists). Nothing in this package ever writes through the
// symlink: the target worktree is read as-is.
func Link(scvHome, path, name string) (*Entry, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("repo link: resolve %s: %w", path, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrPathNotFound, abs)
		}
		return nil, fmt.Errorf("repo link: check %s: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s is not a directory", ErrPathNotFound, abs)
	}
	if name == "" {
		name = filepath.Base(abs)
	}
	if err := validName(name); err != nil {
		return nil, fmt.Errorf("repo link: %w", err)
	}
	reposDir := filepath.Join(scvHome, reposDirName)
	target := filepath.Join(reposDir, name)
	if err := checkVacant(target); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(reposDir, 0o755); err != nil {
		return nil, fmt.Errorf("repo link: create %s: %w", reposDir, err)
	}
	if err := os.Symlink(abs, target); err != nil {
		return nil, fmt.Errorf("repo link: symlink %s -> %s: %w", target, abs, err)
	}
	e := &Entry{Name: name, Path: target, Type: TypeLinked}
	e.URL = remoteURL(target)
	e.Branch = currentBranch(target)
	// Best effort: a dangling link or a worktree without commits has none.
	e.HeadCommit, _ = HeadCommit(target)
	return e, nil
}

// Pull fetches and merges upstream updates for the remote clone named
// name — `git pull origin <current branch>`, the git_op.py move — and
// returns the refreshed Entry. Linked entries are refused (pull applies
// to remote clones only), unknown names are ErrNotFound, and a git
// failure is wrapped with ErrPullFailed.
func Pull(scvHome, name string) (*Entry, error) {
	if err := validName(name); err != nil {
		return nil, fmt.Errorf("repo pull: %w", err)
	}
	target := filepath.Join(scvHome, reposDirName, name)
	info, err := os.Lstat(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: no repository named %q", ErrNotFound, name)
		}
		return nil, fmt.Errorf("repo pull: check %s: %w", target, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("repo pull: %q is a linked entry; pull applies to remote clones only", name)
	}
	branch := currentBranch(target)
	if branch == "" || branch == "HEAD" { // not a worktree, or detached
		branch = DefaultBranch
	}
	if _, err := runGit(target, "pull", "origin", branch); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPullFailed, err)
	}
	e := describe(name, target)
	return &e, nil
}

// List returns every entry under <scvHome>/repos sorted by name: a
// symlink is TypeLinked, a directory with an origin remote is
// TypeRemote, anything else is TypeDir; regular files are skipped. A
// missing repos directory is an empty list, not an error (fresh
// install). Metadata is best-effort per entry.
func List(scvHome string) ([]Entry, error) {
	reposDir := filepath.Join(scvHome, reposDirName)
	dirEntries, err := os.ReadDir(reposDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []Entry{}, nil
		}
		return nil, fmt.Errorf("repo list: read %s: %w", reposDir, err)
	}
	entries := make([]Entry, 0, len(dirEntries)) // ReadDir yields sorted names
	for _, de := range dirEntries {
		name := de.Name()
		path := filepath.Join(reposDir, name)
		// Lstat rather than DirEntry.Type: directory-entry types are
		// 0 (unknown) on filesystems without d_type, which would hide
		// linked entries from the list.
		info, err := os.Lstat(path)
		if err != nil {
			continue // raced with a concurrent removal; not an entry anymore
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			e := Entry{Name: name, Path: path, Type: TypeLinked}
			e.URL = remoteURL(path)
			e.Branch = currentBranch(path)
			e.HeadCommit, _ = HeadCommit(path) // best effort, like every entry field
			entries = append(entries, e)
		case info.IsDir():
			entries = append(entries, describe(name, path))
		}
	}
	return entries, nil
}

// Remove deletes the repos entry named name: a linked entry removes the
// symlink only — the target directory is never touched — and a remote
// entry removes the whole clone directory. The analysis directory
// <scvHome>/analysis/<name> is left in place unless withAnalysis is
// set (spec section 6, audit Q8). Unknown names are refused
// (ErrNotFound).
func Remove(scvHome, name string, withAnalysis bool) error {
	if err := validName(name); err != nil {
		return fmt.Errorf("repo remove: %w", err)
	}
	target := filepath.Join(scvHome, reposDirName, name)
	if _, err := os.Lstat(target); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: no repository named %q", ErrNotFound, name)
		}
		return fmt.Errorf("repo remove: check %s: %w", target, err)
	}
	// os.RemoveAll on a symlink removes the link itself and never
	// descends into the target, so one call serves both entry kinds.
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("repo remove: delete %s: %w", target, err)
	}
	if withAnalysis {
		analysisDir := filepath.Join(scvHome, analysisDirName, name)
		if err := os.RemoveAll(analysisDir); err != nil {
			return fmt.Errorf("repo remove: delete %s: %w", analysisDir, err)
		}
	}
	return nil
}

// HeadCommit returns the full HEAD commit hash of the git worktree at
// path; a symlink path resolves through the link.
func HeadCommit(path string) (string, error) {
	out, err := runGit(path, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("head commit of %s: %w", path, err)
	}
	return out, nil
}

// describe builds the List entry for the directory at path: TypeRemote
// when an origin remote is configured, else TypeDir, with best-effort
// metadata.
func describe(name, path string) Entry {
	e := Entry{Name: name, Path: path, Type: TypeDir}
	if url := remoteURL(path); url != "" {
		e.Type = TypeRemote
		e.URL = url
	}
	e.Branch = currentBranch(path)
	e.HeadCommit, _ = HeadCommit(path) // best effort, like every entry field
	return e
}

// checkVacant refuses an entry path that already exists (ErrNameExists,
// dangling symlinks included) and passes through any other stat error.
func checkVacant(target string) error {
	_, err := os.Lstat(target)
	if err == nil {
		return fmt.Errorf("%w: %q already exists at %s", ErrNameExists, filepath.Base(target), target)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("check %s: %w", target, err)
	}
	return nil
}

// repoNameFromURL derives an entry name from a source url by the
// git_op.py extract_repo_name rules: drop trailing slashes, take the
// last path segment, strip a ".git" suffix.
func repoNameFromURL(url string) string {
	trimmed := strings.TrimRight(url, "/")
	base := trimmed
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		base = trimmed[i+1:]
	}
	return strings.TrimSuffix(base, ".git")
}

// validName rejects entry names that are empty or would escape the
// repos directory (".", "..", or anything containing a separator).
func validName(name string) error {
	if name == "" || name == "." || name == ".." ||
		strings.ContainsRune(name, '/') || strings.ContainsRune(name, filepath.Separator) {
		return fmt.Errorf("invalid repository name %q", name)
	}
	return nil
}

// runGit runs git with args, rooted at dir via -C when dir is not
// empty, and returns trimmed stdout. The child inherits the process
// environment, so git resolves through PATH exactly as before. On
// failure the error carries git's stderr — or stdout when stderr is
// empty — mirroring git_op.py's message selection.
func runGit(dir string, args ...string) (string, error) {
	argv := args
	if dir != "" {
		argv = append([]string{"-C", dir}, args...)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("git", argv...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(argv, " "), msg, err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// remoteURL returns the origin remote URL of the worktree at path, or
// "" when there is no origin remote (or path is not a worktree).
func remoteURL(path string) string {
	out, err := runGit(path, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return out
}

// currentBranch returns the checked-out branch of the worktree at path
// (git_op.py get_current_branch), or "" when it has no resolvable HEAD.
func currentBranch(path string) string {
	out, err := runGit(path, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

// isGitRepo reports whether path is inside a git worktree, by git's own
// verdict: `git -C path rev-parse --git-dir` exits 0. A missing path is
// simply not a git repository.
func isGitRepo(path string) bool {
	if path == "" {
		return false
	}
	_, err := runGit(path, "rev-parse", "--git-dir")
	return err == nil
}
