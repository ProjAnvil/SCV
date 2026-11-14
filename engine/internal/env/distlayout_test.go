package env

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// writeFakeBinary writes a fake prebuilt dist binary at path (parent
// directories created); content identifies the platform it was built
// for so tests can tell selected and decoy pairs apart.
func writeFakeBinary(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// fakePlatformPair installs a fake loader/graphmcp pair for platform
// under distRoot and returns the dist root path.
func fakePlatformPair(t *testing.T, distRoot, platform string) string {
	t.Helper()
	writeFakeBinary(t, filepath.Join(distRoot, platform, "loader", "perception-loader"), "loader-"+platform)
	writeFakeBinary(t, filepath.Join(distRoot, platform, "graphmcp", "scv-graph-mcp"), "graphmcp-"+platform)
	return distRoot
}

// assertSymlink fails unless path is a symlink with exactly the wanted
// target.
func assertSymlink(t *testing.T, path, wantTarget string) {
	t.Helper()
	got, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("readlink %s: %v", path, err)
	}
	if got != wantTarget {
		t.Fatalf("symlink %s -> %s, want -> %s", path, got, wantTarget)
	}
}

func TestDiscoverEnsuringLayout(t *testing.T) {
	t.Run("pair-only dist becomes the flat layout and the flags follow", func(t *testing.T) {
		root := fakePlatformPair(t, t.TempDir(), runtime.GOOS+"-"+runtime.GOARCH)

		snap, err := DiscoverEnsuringLayout(Options{
			ScvHome:  t.TempDir(),
			EnvVars:  isolate("", t.TempDir(), "SCV_PERCEPTION_DIST="+root),
		})
		if err != nil {
			t.Fatalf("DiscoverEnsuringLayout: %v", err)
		}
		if !snap.PrebuiltLoader || !snap.PrebuiltGraphMCP {
			t.Fatalf("Prebuilt = %t/%t, want true/true after the layout selection", snap.PrebuiltLoader, snap.PrebuiltGraphMCP)
		}
		assertSymlink(t, filepath.Join(root, "loader", "perception-loader"),
			filepath.Join("..", runtime.GOOS+"-"+runtime.GOARCH, "loader", "perception-loader"))
		assertSymlink(t, filepath.Join(root, "graphmcp", "scv-graph-mcp"),
			filepath.Join("..", runtime.GOOS+"-"+runtime.GOARCH, "graphmcp", "scv-graph-mcp"))
	})

	t.Run("layout failure still returns the refreshed snapshot", func(t *testing.T) {
		root := fakePlatformPair(t, t.TempDir(), runtime.GOOS+"-"+runtime.GOARCH)
		// dist/loader as a regular file makes the symlink swap fail.
		if err := os.WriteFile(filepath.Join(root, "loader"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}

		snap, err := DiscoverEnsuringLayout(Options{
			ScvHome: t.TempDir(),
			EnvVars: isolate("", t.TempDir(), "SCV_PERCEPTION_DIST="+root),
		})
		if err == nil {
			t.Fatal("DiscoverEnsuringLayout over a broken layout: want error, got nil")
		}
		if snap == nil {
			t.Fatal("layout failure lost the snapshot, want it for diagnostic callers")
		}
		if snap.DistRoot != root {
			t.Fatalf("DistRoot = %q, want %q", snap.DistRoot, root)
		}
		// The refreshed flags honestly report the unusable flat layout.
		if snap.PrebuiltLoader {
			t.Error("PrebuiltLoader = true despite the failed layout")
		}
	})

	t.Run("discovery failure returns nil", func(t *testing.T) {
		snap, err := DiscoverEnsuringLayout(Options{EnvVars: []string{"PATH=", "HOME="}})
		if err == nil || snap != nil {
			t.Fatalf("unresolvable home: (%p, %v), want (nil, error)", snap, err)
		}
	})
}

func TestEnsureDistLayoutSelectsPlatform(t *testing.T) {
	root := fakePlatformPair(t, t.TempDir(), "linux-amd64")
	// A decoy pair for another platform must never be selected.
	fakePlatformPair(t, root, "linux-arm64")

	if err := ensureDistLayout(root, "linux-amd64", layoutLockStale); err != nil {
		t.Fatalf("ensureDistLayout: %v", err)
	}

	assertSymlink(t, filepath.Join(root, "loader", "perception-loader"),
		"../linux-amd64/loader/perception-loader")
	assertSymlink(t, filepath.Join(root, "graphmcp", "scv-graph-mcp"),
		"../linux-amd64/graphmcp/scv-graph-mcp")

	// The links resolve to the selected platform's binaries.
	if got, err := os.ReadFile(filepath.Join(root, "loader", "perception-loader")); err != nil || string(got) != "loader-linux-amd64" {
		t.Fatalf("loader link resolves to %q (err %v), want loader-linux-amd64", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "graphmcp", "scv-graph-mcp")); err != nil || string(got) != "graphmcp-linux-amd64" {
		t.Fatalf("graphmcp link resolves to %q (err %v), want graphmcp-linux-amd64", got, err)
	}

	// A released lock leaves nothing behind.
	if _, err := os.Lstat(filepath.Join(root, layoutLockName)); !os.IsNotExist(err) {
		t.Fatalf("layout lock still present after release: %v", err)
	}
}

func TestEnsureDistLayoutCurrentPlatform(t *testing.T) {
	// The exported wrapper must select the runtime's own platform id.
	root := fakePlatformPair(t, t.TempDir(), runtime.GOOS+"-"+runtime.GOARCH)

	if err := EnsureDistLayout(root); err != nil {
		t.Fatalf("EnsureDistLayout: %v", err)
	}
	assertSymlink(t, filepath.Join(root, "loader", "perception-loader"),
		filepath.Join("..", runtime.GOOS+"-"+runtime.GOARCH, "loader", "perception-loader"))
}

func TestEnsureDistLayoutIdempotent(t *testing.T) {
	root := fakePlatformPair(t, t.TempDir(), "linux-amd64")
	link := filepath.Join(root, "loader", "perception-loader")
	if err := ensureDistLayout(root, "linux-amd64", layoutLockStale); err != nil {
		t.Fatalf("first ensureDistLayout: %v", err)
	}
	before, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	time.Sleep(20 * time.Millisecond) // a rewrite would stamp a later mtime

	if err := ensureDistLayout(root, "linux-amd64", layoutLockStale); err != nil {
		t.Fatalf("second ensureDistLayout: %v", err)
	}
	after, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("idempotent call rewrote the link: mtime %v -> %v", before.ModTime(), after.ModTime())
	}
}

func TestEnsureDistLayoutReplacesForeignLayout(t *testing.T) {
	root := fakePlatformPair(t, t.TempDir(), "linux-amd64")
	// A stale link to another platform...
	if err := os.MkdirAll(filepath.Join(root, "loader"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../linux-arm64/loader/perception-loader", filepath.Join(root, "loader", "perception-loader")); err != nil {
		t.Fatal(err)
	}
	// ...and a copied regular binary in the graphmcp slot.
	writeFakeBinary(t, filepath.Join(root, "graphmcp", "scv-graph-mcp"), "old-copy")

	if err := ensureDistLayout(root, "linux-amd64", layoutLockStale); err != nil {
		t.Fatalf("ensureDistLayout: %v", err)
	}
	assertSymlink(t, filepath.Join(root, "loader", "perception-loader"),
		"../linux-amd64/loader/perception-loader")
	assertSymlink(t, filepath.Join(root, "graphmcp", "scv-graph-mcp"),
		"../linux-amd64/graphmcp/scv-graph-mcp")
}

func TestEnsureDistLayoutNoopWithoutPlatformPair(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dist") // deliberately not created

	if err := ensureDistLayout(root, "linux-amd64", layoutLockStale); err != nil {
		t.Fatalf("ensureDistLayout: %v", err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("dist root created by a no-op selection: %v", err)
	}
}

func TestEnsureDistLayoutPartialPair(t *testing.T) {
	// A damaged pack may carry only one binary of the pair; the present
	// half is still selected.
	root := t.TempDir()
	writeFakeBinary(t, filepath.Join(root, "linux-amd64", "graphmcp", "scv-graph-mcp"), "graphmcp-linux-amd64")

	if err := ensureDistLayout(root, "linux-amd64", layoutLockStale); err != nil {
		t.Fatalf("ensureDistLayout: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "loader")); !os.IsNotExist(err) {
		t.Fatalf("loader dir created without a source binary: %v", err)
	}
	assertSymlink(t, filepath.Join(root, "graphmcp", "scv-graph-mcp"),
		"../linux-amd64/graphmcp/scv-graph-mcp")
}

func TestEnsureDistLayoutTakesOverStaleLock(t *testing.T) {
	root := fakePlatformPair(t, t.TempDir(), "linux-amd64")
	// A crashed holder's lock: present, but far older than the stale
	// window.
	stale := 200 * time.Millisecond
	lockPath := filepath.Join(root, layoutLockName)
	writeFakeBinary(t, lockPath, "pid=1 acquired=1\n")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}

	if err := ensureDistLayout(root, "linux-amd64", stale); err != nil {
		t.Fatalf("ensureDistLayout over stale lock: %v", err)
	}
	assertSymlink(t, filepath.Join(root, "loader", "perception-loader"),
		"../linux-amd64/loader/perception-loader")
	if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("stale lock not cleaned up: %v", err)
	}
}

func TestEnsureDistLayoutWaitsForLiveLock(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the directory permissions this test relies on")
	}
	root := fakePlatformPair(t, t.TempDir(), "linux-amd64")
	// A live foreign lock: fresh mtime, so a waiter must back off until
	// it crosses the stale window and takes over.
	stale := 250 * time.Millisecond
	writeFakeBinary(t, filepath.Join(root, layoutLockName), "pid=1 acquired=1\n")

	start := time.Now()
	if err := ensureDistLayout(root, "linux-amd64", stale); err != nil {
		t.Fatalf("ensureDistLayout behind live lock: %v", err)
	}
	if waited := time.Since(start); waited < stale {
		t.Fatalf("took over a live lock after only %v (stale window %v)", waited, stale)
	}
	assertSymlink(t, filepath.Join(root, "loader", "perception-loader"),
		"../linux-amd64/loader/perception-loader")
}

func TestEnsureDistLayoutLinkErrors(t *testing.T) {
	t.Run("nonempty dir in the link slot", func(t *testing.T) {
		root := fakePlatformPair(t, t.TempDir(), "linux-amd64")
		// os.Remove refuses a nonempty directory, so the swap must fail
		// loudly instead of deleting unknown content.
		nested := filepath.Join(root, "loader", "perception-loader", "keep")
		writeFakeBinary(t, nested, "data")

		err := ensureDistLayout(root, "linux-amd64", layoutLockStale)
		if err == nil {
			t.Fatal("ensureDistLayout over a nonempty dir: want error, got nil")
		}
		if _, readErr := os.ReadFile(nested); readErr != nil {
			t.Fatalf("nonempty dir content was destroyed: %v", readErr)
		}
	})

	t.Run("file where a link parent dir belongs", func(t *testing.T) {
		root := fakePlatformPair(t, t.TempDir(), "linux-amd64")
		// dist/loader exists as a regular file: MkdirAll must fail.
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "loader"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := ensureDistLayout(root, "linux-amd64", layoutLockStale); err == nil {
			t.Fatal("ensureDistLayout with a file at dist/loader: want error, got nil")
		}
	})

	t.Run("unwritable link dir", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores the directory permissions this test relies on")
		}
		root := fakePlatformPair(t, t.TempDir(), "linux-amd64")
		if err := os.MkdirAll(filepath.Join(root, "loader"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(root, "loader"), 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "loader"), 0o755) })

		if err := ensureDistLayout(root, "linux-amd64", layoutLockStale); err == nil {
			t.Fatal("ensureDistLayout into an unwritable dir: want error, got nil")
		}
	})
}

func TestAcquireLayoutLockErrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the directory permissions this test relies on")
	}
	t.Run("unwritable parent", func(t *testing.T) {
		ro := filepath.Join(t.TempDir(), "ro")
		if err := os.Mkdir(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })

		if _, err := acquireLayoutLock(filepath.Join(ro, "dist"), layoutLockStale); err == nil {
			t.Fatal("acquireLayoutLock under an unwritable parent: want error, got nil")
		}
	})

	t.Run("lock create refused", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Chmod(root, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

		if _, err := acquireLayoutLock(root, layoutLockStale); err == nil {
			t.Fatal("acquireLayoutLock in an unwritable dist root: want error, got nil")
		}
	})
}

func TestLayoutLockReleaseGuardsToken(t *testing.T) {
	root := t.TempDir()
	lock, err := acquireLayoutLock(root, layoutLockStale)
	if err != nil {
		t.Fatalf("acquireLayoutLock: %v", err)
	}
	lockPath := filepath.Join(root, layoutLockName)

	// A taken-over lock no longer belongs to us: release must leave it.
	if err := os.WriteFile(lockPath, []byte("pid=999 acquired=999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lock.release()
	if _, err := os.Lstat(lockPath); err != nil {
		t.Fatalf("foreign lock file removed by release: %v", err)
	}
	// Releasing twice is a no-op.
	lock.release()

	// The foreign holder finishes and drops its lock; a fresh acquire
	// then succeeds immediately and releases cleanly.
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	lock2, err := acquireLayoutLock(root, layoutLockStale)
	if err != nil {
		t.Fatalf("reacquireLayoutLock: %v", err)
	}
	lock2.release()
	if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("own lock file kept after release: %v", err)
	}
}
