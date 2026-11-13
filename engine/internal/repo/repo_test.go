package repo

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// testGit runs git with hermetic configuration (no user or system
// gitconfig, pinned identity) so fixtures behave identically on every
// host; it fails the test on any non-zero exit. dir "" runs git in the
// current directory, otherwise git runs with -C dir.
func testGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	argv := args
	if dir != "" {
		argv = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", argv...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=SCV Test",
		"GIT_AUTHOR_EMAIL=scv-test@example.com",
		"GIT_COMMITTER_NAME=SCV Test",
		"GIT_COMMITTER_EMAIL=scv-test@example.com",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(argv, " "), err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}

// newFixtureRepo creates a git repository with one empty commit on
// branch main and returns its absolute path.
func newFixtureRepo(t *testing.T) string {
	t.Helper()
	return newFixtureRepoAt(t, t.TempDir())
}

// newFixtureRepoAt is newFixtureRepo at an explicit path, so tests can
// control the directory (and therefore repository) name.
func newFixtureRepoAt(t *testing.T, path string) string {
	t.Helper()
	testGit(t, "", "init", "-b", "main", path)
	testGit(t, path, "commit", "--allow-empty", "-m", "init")
	return path
}

// newFixtureRepoOnBranch is newFixtureRepo with the initial branch
// named branch instead of main.
func newFixtureRepoOnBranch(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	testGit(t, "", "init", "-b", branch, dir)
	testGit(t, dir, "commit", "--allow-empty", "-m", "init")
	return dir
}

// fixtureHead returns the current HEAD commit of a fixture repository.
func fixtureHead(t *testing.T, dir string) string {
	t.Helper()
	return testGit(t, dir, "rev-parse", "HEAD")
}

// writeAnalysisMarker creates <scvHome>/analysis/<name>/marker so tests
// can observe whether the analysis directory was kept or deleted.
func writeAnalysisMarker(t *testing.T, scvHome, name string) {
	t.Helper()
	dir := filepath.Join(scvHome, "analysis", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "marker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// pathExists reports whether path exists (lstat, so dangling symlinks
// count as existing).
func pathExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

// isSymlink reports whether path is a symlink.
func isSymlink(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()&os.ModeSymlink != 0
}

func TestClassify(t *testing.T) {
	gitRepo := newFixtureRepo(t)
	plainDir := t.TempDir()
	tests := []struct {
		name   string
		source string
		want   SourceKind
	}{
		{"https url", "https://github.com/org/repo.git", KindURL},
		{"https url without .git", "https://github.com/org/repo", KindURL},
		{"ssh scheme url", "ssh://git@host:2222/org/repo.git", KindURL},
		{"git scheme url", "git://host/org/repo.git", KindURL},
		{"scp-like url", "git@github.com:org/repo.git", KindURL},
		{"local git repo", gitRepo, KindLocalGit},
		{"local plain directory", plainDir, KindLocalDir},
		{"missing local path", filepath.Join(t.TempDir(), "nope"), KindLocalDir},
		{"empty source", "", KindLocalDir},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.source); got != tt.want {
				t.Errorf("Classify(%q) = %q, want %q", tt.source, got, tt.want)
			}
		})
	}
}

func TestRepoNameFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/org/repo.git", "repo"},
		{"https://github.com/org/repo", "repo"},
		{"https://github.com/org/repo/", "repo"},
		{"git@github.com:org/repo.git", "repo"},
		{"https://github.com/org/double.git.git", "double.git"},
	}
	for _, tt := range tests {
		if got := repoNameFromURL(tt.url); got != tt.want {
			t.Errorf("repoNameFromURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestValidName(t *testing.T) {
	for _, name := range []string{"", ".", "..", "a/b", "/abs"} {
		if err := validName(name); err == nil {
			t.Errorf("validName(%q) accepted an escaping or empty name", name)
		}
	}
	for _, name := range []string{"repo", "my proj", "a.git"} {
		if err := validName(name); err != nil {
			t.Errorf("validName(%q) = %v, want nil", name, err)
		}
	}
}

func TestCloneAndList(t *testing.T) {
	fixture := newFixtureRepo(t)
	head := fixtureHead(t, fixture)
	scvHome := t.TempDir()
	name := filepath.Base(fixture)

	// Empty branch means the default, main.
	e, err := Clone(scvHome, fixture, "")
	if err != nil {
		t.Fatal(err)
	}
	want := Entry{
		Name:       name,
		Path:       filepath.Join(scvHome, "repos", name),
		URL:        fixture,
		Branch:     "main",
		Type:       TypeRemote,
		HeadCommit: head,
	}
	if *e != want {
		t.Errorf("Clone entry = %+v, want %+v", *e, want)
	}

	entries, err := List(scvHome)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0] != want {
		t.Errorf("List = %+v, want the single entry %+v", entries, want)
	}

	// Cloning under an existing name refuses and clobbers nothing.
	if _, err := Clone(scvHome, fixture, "main"); !errors.Is(err, ErrNameExists) {
		t.Errorf("second clone error = %v, want ErrNameExists", err)
	}

	// A missing source fails as CLONE_FAILED and leaves no entry.
	other := t.TempDir()
	if _, err := Clone(other, filepath.Join(t.TempDir(), "missing"), ""); !errors.Is(err, ErrCloneFailed) {
		t.Errorf("clone of missing source error = %v, want ErrCloneFailed", err)
	}
	if entries, _ := List(other); len(entries) != 0 {
		t.Errorf("List after failed clone = %+v, want empty", entries)
	}

	// A source with no derivable name fails the same way.
	if _, err := Clone(t.TempDir(), "", ""); !errors.Is(err, ErrCloneFailed) {
		t.Errorf("clone of empty source error = %v, want ErrCloneFailed", err)
	}
}

func TestCloneExplicitBranch(t *testing.T) {
	trunk := newFixtureRepoOnBranch(t, "trunk")
	scvHome := t.TempDir()

	e, err := Clone(scvHome, trunk, "trunk")
	if err != nil {
		t.Fatal(err)
	}
	if e.Branch != "trunk" || e.HeadCommit != fixtureHead(t, trunk) {
		t.Errorf("clone on trunk = %+v", *e)
	}

	// The default branch main does not exist there: CLONE_FAILED, no entry.
	other := t.TempDir()
	if _, err := Clone(other, trunk, ""); !errors.Is(err, ErrCloneFailed) {
		t.Errorf("clone with default branch onto trunk repo error = %v, want ErrCloneFailed", err)
	}
	if entries, _ := List(other); len(entries) != 0 {
		t.Errorf("List after branch miss = %+v, want empty", entries)
	}
}

func TestLinkAndList(t *testing.T) {
	fixture := newFixtureRepo(t)
	head := fixtureHead(t, fixture)
	scvHome := t.TempDir()
	name := filepath.Base(fixture)

	// Empty name defaults to the base name of the resolved target.
	e, err := Link(scvHome, fixture, "")
	if err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(scvHome, "repos", name)
	if e.Name != name || e.Type != TypeLinked || e.Path != linkPath {
		t.Errorf("Link entry = %+v", *e)
	}
	if !isSymlink(t, linkPath) {
		t.Fatalf("%s is not a symlink", linkPath)
	}
	if target, err := os.Readlink(linkPath); err != nil || target != fixture {
		t.Errorf("symlink target = %q (err %v), want the absolute path %q", target, err, fixture)
	}
	// The fixture has no origin remote: URL stays empty, git metadata flows through.
	if e.URL != "" || e.Branch != "main" || e.HeadCommit != head {
		t.Errorf("Link entry metadata = %+v", *e)
	}

	// Linking a local clone carries its origin URL through the symlink.
	origin := newFixtureRepo(t)
	localClone := filepath.Join(t.TempDir(), "localclone")
	testGit(t, "", "clone", origin, localClone)
	linked, err := Link(scvHome, localClone, "withremote")
	if err != nil {
		t.Fatal(err)
	}
	if linked.URL != origin {
		t.Errorf("linked clone URL = %q, want %q", linked.URL, origin)
	}

	// Explicit names, missing paths, non-directories, escaping names.
	if e, err := Link(scvHome, t.TempDir(), "alias"); err != nil || e.Name != "alias" {
		t.Errorf("Link with explicit name = %+v, err %v", e, err)
	}
	if _, err := Link(scvHome, filepath.Join(t.TempDir(), "missing"), "m"); !errors.Is(err, ErrPathNotFound) {
		t.Errorf("Link of missing path error = %v, want ErrPathNotFound", err)
	}
	regularFile := filepath.Join(t.TempDir(), "regular")
	if err := os.WriteFile(regularFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Link(scvHome, regularFile, "f"); !errors.Is(err, ErrPathNotFound) {
		t.Errorf("Link of regular file error = %v, want ErrPathNotFound", err)
	}
	if _, err := Link(scvHome, t.TempDir(), "../escape"); err == nil {
		t.Error("Link with an escaping name succeeded")
	}

	// List shows the linked entries, sorted by name.
	entries, err := List(scvHome)
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{name, "alias", "withremote"} // fixture base sorts first (uppercase test prefix)
	if len(entries) != len(wantNames) {
		t.Fatalf("List = %+v, want %d entries", entries, len(wantNames))
	}
	for i, e := range entries {
		if e.Name != wantNames[i] || e.Type != TypeLinked {
			t.Errorf("List[%d] = %+v, want linked entry named %q", i, e, wantNames[i])
		}
	}
}

func TestLinkNameCollisionRefuses(t *testing.T) {
	scvHome := t.TempDir()
	cloneSrc := newFixtureRepo(t)
	e, err := Clone(scvHome, cloneSrc, "")
	if err != nil {
		t.Fatal(err)
	}

	// Linking a different repository under the clone's name refuses.
	other := newFixtureRepo(t)
	if _, err := Link(scvHome, other, e.Name); !errors.Is(err, ErrNameExists) {
		t.Errorf("Link over existing clone error = %v, want ErrNameExists", err)
	}
	// The clone is untouched: still a directory, still the same clone.
	if isSymlink(t, e.Path) {
		t.Fatalf("%s became a symlink after the refused link", e.Path)
	}
	entries, err := List(scvHome)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Type != TypeRemote || entries[0].HeadCommit != e.HeadCommit {
		t.Errorf("entries after refused link = %+v, want the untouched clone", entries)
	}

	// The mirror image: cloning onto an existing linked name refuses too.
	home2 := t.TempDir()
	linkedSrc := newFixtureRepo(t)
	if _, err := Link(home2, linkedSrc, "taken"); err != nil {
		t.Fatal(err)
	}
	cloneNamed := newFixtureRepoAt(t, filepath.Join(t.TempDir(), "taken"))
	if _, err := Clone(home2, cloneNamed, "main"); !errors.Is(err, ErrNameExists) {
		t.Errorf("Clone over existing link error = %v, want ErrNameExists", err)
	}
	if target, err := os.Readlink(filepath.Join(home2, "repos", "taken")); err != nil || target != linkedSrc {
		t.Errorf("link after refused clone = %q (err %v), want %q", target, err, linkedSrc)
	}
}

func TestNamesWithSpacesRoundTrip(t *testing.T) {
	root := t.TempDir()
	fixture := newFixtureRepoAt(t, filepath.Join(root, "my proj"))
	head := fixtureHead(t, fixture)
	scvHome := t.TempDir()

	// Clone derives and keeps the spaced name.
	e, err := Clone(scvHome, fixture, "")
	if err != nil {
		t.Fatalf("clone of repo named %q: %v", "my proj", err)
	}
	if e.Name != "my proj" {
		t.Errorf("clone name = %q, want %q", e.Name, "my proj")
	}
	entries, err := List(scvHome)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "my proj" || entries[0].HeadCommit != head {
		t.Errorf("List of spaced name = %+v", entries)
	}

	if err := Remove(scvHome, "my proj", false); err != nil {
		t.Fatalf("remove of spaced name: %v", err)
	}
	if pathExists(t, filepath.Join(scvHome, "repos", "my proj")) {
		t.Error("clone with spaced name not removed")
	}

	// The same name round-trips through link.
	e, err = Link(scvHome, fixture, "")
	if err != nil || e.Name != "my proj" {
		t.Fatalf("link of repo named %q = %+v, err %v", "my proj", e, err)
	}
	if err := Remove(scvHome, "my proj", false); err != nil {
		t.Fatalf("remove of spaced linked name: %v", err)
	}
}

func TestRemoveLinkedLeavesTarget(t *testing.T) {
	fixture := newFixtureRepo(t)
	head := fixtureHead(t, fixture)
	scvHome := t.TempDir()
	e, err := Link(scvHome, fixture, "linked")
	if err != nil {
		t.Fatal(err)
	}
	writeAnalysisMarker(t, scvHome, "linked")

	if err := Remove(scvHome, "linked", false); err != nil {
		t.Fatal(err)
	}
	if pathExists(t, e.Path) {
		t.Error("repos entry still exists after remove")
	}
	// The target directory is untouched: still a repository at the same head.
	if !pathExists(t, filepath.Join(fixture, ".git")) {
		t.Error("remove of a linked entry touched the target's .git")
	}
	if got := fixtureHead(t, fixture); got != head {
		t.Errorf("target head = %s after remove, want %s", got, head)
	}
	// The analysis directory stays unless explicitly requested.
	if !pathExists(t, filepath.Join(scvHome, "analysis", "linked", "marker")) {
		t.Error("analysis removed without withAnalysis")
	}

	// Unknown names are refused.
	if err := Remove(scvHome, "linked", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("remove of unknown name error = %v, want ErrNotFound", err)
	}

	// withAnalysis opts in to deleting the analysis directory — and only that.
	if _, err := Link(scvHome, fixture, "linked"); err != nil {
		t.Fatal(err)
	}
	if err := Remove(scvHome, "linked", true); err != nil {
		t.Fatal(err)
	}
	if pathExists(t, filepath.Join(scvHome, "analysis", "linked")) {
		t.Error("analysis kept despite withAnalysis")
	}
	if !pathExists(t, filepath.Join(fixture, ".git")) {
		t.Error("withAnalysis remove touched the target")
	}
}

func TestRemoveCloneAndAnalysis(t *testing.T) {
	fixture := newFixtureRepo(t)
	scvHome := t.TempDir()
	e, err := Clone(scvHome, fixture, "")
	if err != nil {
		t.Fatal(err)
	}
	writeAnalysisMarker(t, scvHome, e.Name)

	// Default: the clone goes, the analysis stays.
	if err := Remove(scvHome, e.Name, false); err != nil {
		t.Fatal(err)
	}
	if pathExists(t, e.Path) {
		t.Error("clone directory still exists after remove")
	}
	if !pathExists(t, filepath.Join(scvHome, "analysis", e.Name, "marker")) {
		t.Error("analysis removed without withAnalysis")
	}

	// withAnalysis removes both.
	if _, err := Clone(scvHome, fixture, ""); err != nil {
		t.Fatal(err)
	}
	if err := Remove(scvHome, e.Name, true); err != nil {
		t.Fatal(err)
	}
	if pathExists(t, filepath.Join(scvHome, "analysis", e.Name)) {
		t.Error("analysis kept despite withAnalysis")
	}

	// Unknown and escaping names are refused.
	if err := Remove(scvHome, e.Name, false); !errors.Is(err, ErrNotFound) {
		t.Errorf("remove of unknown name error = %v, want ErrNotFound", err)
	}
	if err := Remove(scvHome, "../evil", false); err == nil {
		t.Error("remove accepted an escaping name")
	}
}

func TestPullUpdatesEntry(t *testing.T) {
	fixture := newFixtureRepo(t)
	scvHome := t.TempDir()
	e, err := Clone(scvHome, fixture, "")
	if err != nil {
		t.Fatal(err)
	}

	// Advance upstream; pull must land on the new head.
	testGit(t, fixture, "commit", "--allow-empty", "-m", "second")
	newHead := fixtureHead(t, fixture)
	pulled, err := Pull(scvHome, e.Name)
	if err != nil {
		t.Fatal(err)
	}
	if pulled.HeadCommit != newHead {
		t.Errorf("pulled head = %s, want %s", pulled.HeadCommit, newHead)
	}
	if pulled.Type != TypeRemote || pulled.URL != fixture || pulled.Branch != "main" {
		t.Errorf("pulled entry = %+v", *pulled)
	}

	// Unknown names refuse.
	if _, err := Pull(scvHome, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("pull of unknown name error = %v, want ErrNotFound", err)
	}
	// Linked entries are remote-only.
	if _, err := Link(scvHome, newFixtureRepo(t), "linkedentry"); err != nil {
		t.Fatal(err)
	}
	if _, err := Pull(scvHome, "linkedentry"); err == nil {
		t.Error("pull of a linked entry succeeded")
	}
	// A non-git directory entry cannot be pulled.
	if err := os.MkdirAll(filepath.Join(scvHome, "repos", "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Pull(scvHome, "plain"); !errors.Is(err, ErrPullFailed) {
		t.Errorf("pull of non-git entry error = %v, want ErrPullFailed", err)
	}
	// Escaping names refuse before touching the filesystem.
	if _, err := Pull(scvHome, "a/b"); err == nil {
		t.Error("pull accepted an escaping name")
	}
}

func TestHeadCommit(t *testing.T) {
	fixture := newFixtureRepo(t)
	head, err := HeadCommit(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(head) {
		t.Errorf("HeadCommit = %q, want a 40-hex commit id", head)
	}
	if _, err := HeadCommit(t.TempDir()); err == nil {
		t.Error("HeadCommit of a non-repository directory succeeded")
	}
}

func TestListFreshHomeIsEmpty(t *testing.T) {
	entries, err := List(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("List of a fresh scv home = %+v, want empty", entries)
	}
}

func TestListForeignEntries(t *testing.T) {
	scvHome := t.TempDir()
	repos := filepath.Join(scvHome, "repos")
	if err := os.MkdirAll(filepath.Join(repos, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repos, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := List(scvHome)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("List = %+v, want only the plain directory entry", entries)
	}
	e := entries[0]
	if e.Name != "plain" || e.Type != TypeDir || e.URL != "" || e.Branch != "" || e.HeadCommit != "" {
		t.Errorf("dir entry = %+v, want a metadata-empty TypeDir entry", e)
	}
}

func TestCloneLinkRejectUnusableScvHome(t *testing.T) {
	fixture := newFixtureRepo(t)
	// A file where the repos directory should live: every entry check fails.
	fileHome := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(fileHome, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Clone(fileHome, fixture, ""); err == nil || errors.Is(err, ErrCloneFailed) {
		t.Errorf("Clone into file-backed scv home error = %v, want a non-clone filesystem error", err)
	}
	if _, err := Link(fileHome, fixture, "x"); err == nil || errors.Is(err, ErrPathNotFound) {
		t.Errorf("Link into file-backed scv home error = %v, want a non-path filesystem error", err)
	}
	if err := Remove(fileHome, "x", false); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("Remove in file-backed scv home error = %v, want a filesystem error", err)
	}
	if _, err := Pull(fileHome, "x"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("Pull in file-backed scv home error = %v, want a filesystem error", err)
	}
}

func TestRemoveReportsDeletionFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based deletion failures cannot be triggered")
	}
	fixture := newFixtureRepo(t)
	scvHome := t.TempDir()
	repos := filepath.Join(scvHome, "repos")
	analysis := filepath.Join(scvHome, "analysis")

	// A read-only repos directory blocks entry deletion.
	if _, err := Link(scvHome, fixture, "linked"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(repos, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(repos, 0o755) })
	if err := Remove(scvHome, "linked", false); err == nil {
		t.Error("remove from a read-only repos directory succeeded")
	}

	// A read-only analysis parent blocks the withAnalysis deletion and
	// is reported even though the entry itself went away.
	if err := os.Chmod(repos, 0o755); err != nil {
		t.Fatal(err)
	}
	// The failed remove above left the entry in place; clear it first.
	if err := Remove(scvHome, "linked", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Link(scvHome, fixture, "linked"); err != nil {
		t.Fatal(err)
	}
	writeAnalysisMarker(t, scvHome, "linked")
	if err := os.Chmod(analysis, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(analysis, 0o755) })
	if err := Remove(scvHome, "linked", true); err == nil {
		t.Error("withAnalysis removal under a read-only analysis directory succeeded")
	}
	if pathExists(t, filepath.Join(scvHome, "repos", "linked")) {
		t.Error("entry not removed before the analysis failure")
	}
	if !pathExists(t, filepath.Join(scvHome, "analysis", "linked")) {
		t.Error("analysis removed despite the read-only parent")
	}
}
