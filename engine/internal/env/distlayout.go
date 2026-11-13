package env

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// distLayoutBinaries lists the prebuilt binaries of one platform pair
// (spec section 11): the subdirectory under dist/ and the binary name
// inside it.
var distLayoutBinaries = [...]struct{ dir, name string }{
	{"loader", "perception-loader"},
	{"graphmcp", "scv-graph-mcp"},
}

// Layout lock (spec section 11, "locked like section 10"): the lock
// file name under the dist root, the age at which a lock belongs to a
// crashed holder and is taken over, and the retry tick between
// attempts — perceive.py's dist/.build.lock cadence.
const (
	layoutLockName  = ".layout.lock"
	layoutLockStale = 10 * time.Minute
	layoutLockRetry = 50 * time.Millisecond
)

// EnsureDistLayout selects the current platform's prebuilt dist pair
// (spec section 11): when the runtime pack installed
// dist/<goos>-<goarch>/{loader,graphmcp} binaries, dist/loader/ and
// dist/graphmcp/ receive relative symlinks to that pair — the flat
// layout perceive.py and the env gate discover. When no binary exists
// for the platform (a dev checkout without a pack) the call is a no-op
// and the gate's binary-OR-toolchain rule keeps deciding; the swap
// runs under the O_EXCL layout lock. Only filesystem failures are
// errors.
func EnsureDistLayout(distRoot string) error {
	return ensureDistLayout(distRoot, runtime.GOOS+"-"+runtime.GOARCH, layoutLockStale)
}

// DiscoverEnsuringLayout is the production discovery entry point (spec
// section 11): Discover, then EnsureDistLayout on the discovered dist
// root, then a refresh of the prebuilt flags so the snapshot the caller
// gates on reflects the pair the layout selection just installed — a
// no-Go host that unpacked only the platform-pair dist is reported with
// the prebuilts present, not degraded to go-missing.
//
// Error contract: a discovery failure returns (nil, err) unchanged; a
// layout failure returns the refreshed snapshot TOGETHER with the error,
// so diagnostic callers (`scv env`) can still report the snapshot while
// the run pipeline fails its checking stage on the error.
func DiscoverEnsuringLayout(opts Options) (*Env, error) {
	snap, err := Discover(opts)
	if err != nil {
		return nil, err
	}
	lerr := EnsureDistLayout(snap.DistRoot)
	snap.PrebuiltLoader = fileExists(filepath.Join(snap.DistRoot, "loader", "perception-loader"))
	snap.PrebuiltGraphMCP = fileExists(filepath.Join(snap.DistRoot, "graphmcp", "scv-graph-mcp"))
	if lerr != nil {
		return snap, fmt.Errorf("ensure dist layout: %w", lerr)
	}
	return snap, nil
}

// ensureDistLayout is EnsureDistLayout with the platform id and the
// lock's stale window injectable for tests.
func ensureDistLayout(distRoot, platform string, stale time.Duration) error {
	var plan []distLink
	for _, b := range distLayoutBinaries {
		source := filepath.Join(distRoot, platform, b.dir, b.name)
		if !fileExists(source) {
			continue
		}
		plan = append(plan, distLink{
			link:   filepath.Join(distRoot, b.dir, b.name),
			target: filepath.Join("..", platform, b.dir, b.name),
		})
	}
	if len(plan) == 0 {
		return nil
	}

	lock, err := acquireLayoutLock(distRoot, stale)
	if err != nil {
		return err
	}
	defer lock.release()
	for _, l := range plan {
		if err := l.ensure(); err != nil {
			return err
		}
	}
	return nil
}

// distLink is one symlink to place: link is the flat-layout path,
// target the platform-pair path relative to the link's directory, so
// the perception root stays relocatable.
type distLink struct {
	link   string
	target string
}

// ensure replaces link with a symlink to target unless it already is
// exactly that symlink (idempotent). Anything else at the path — a
// stale link or a copied binary — is replaced: once the packed pair
// exists it is the single source of truth. os.Remove (not RemoveAll)
// refuses a nonempty directory instead of destroying unknown content.
func (l distLink) ensure() error {
	if current, err := os.Readlink(l.link); err == nil && current == l.target {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.link), 0o755); err != nil {
		return fmt.Errorf("env: create %s: %w", filepath.Dir(l.link), err)
	}
	if err := os.Remove(l.link); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("env: replace link %s: %w", l.link, err)
	}
	if err := os.Symlink(l.target, l.link); err != nil {
		return fmt.Errorf("env: symlink %s -> %s: %w", l.link, l.target, err)
	}
	return nil
}

// acquireLayoutLock takes dist/.layout.lock exclusively and returns the
// holder. The O_EXCL lockfile with stale takeover is perceive.py's
// _DistBuildLock pattern (chosen over the run package's flock: plain os
// calls need no build-tagged platform split, and run imports this
// package). A contended lock older than stale belonged to a crashed
// holder and is taken over (unlink + retry), so waiters always make
// progress without an explicit timeout.
func acquireLayoutLock(distRoot string, stale time.Duration) (*layoutLock, error) {
	if err := os.MkdirAll(distRoot, 0o755); err != nil {
		return nil, fmt.Errorf("env: create dist root: %w", err)
	}
	l := &layoutLock{path: filepath.Join(distRoot, layoutLockName)}
	for !l.held {
		err := l.tryAcquire()
		switch {
		case err == nil:
		case errors.Is(err, fs.ErrExist):
			l.waitOrTakeOver(stale)
		default:
			return nil, fmt.Errorf("env: acquire %s: %w", l.path, err)
		}
	}
	return l, nil
}

// layoutLock is one holder of dist/.layout.lock.
type layoutLock struct {
	path  string
	token string
	held  bool
}

// tryAcquire creates the lock file exclusively and stamps it with this
// holder's token.
func (l *layoutLock) tryAcquire() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	l.token = fmt.Sprintf("pid=%d acquired=%d\n", os.Getpid(), time.Now().Unix())
	defer f.Close()
	if _, err := f.WriteString(l.token); err != nil {
		return err
	}
	l.held = true
	return nil
}

// waitOrTakeOver backs off one retry tick; a lock whose mtime is older
// than stale is unlinked first (best effort — a lost takeover race
// only makes the next O_EXCL attempt fail again, and a lock that
// vanished between the failed create and the stat just retries).
func (l *layoutLock) waitOrTakeOver(stale time.Duration) {
	if info, err := os.Stat(l.path); err == nil && time.Since(info.ModTime()) > stale {
		_ = os.Remove(l.path)
	}
	time.Sleep(layoutLockRetry)
}

// release drops the lock: the file is unlinked only while its content
// is still this holder's token, so a lock taken over as stale belongs
// to its new holder.
func (l *layoutLock) release() {
	if !l.held {
		return
	}
	l.held = false
	content, err := os.ReadFile(l.path)
	if err != nil || string(content) != l.token {
		return
	}
	_ = os.Remove(l.path)
}
