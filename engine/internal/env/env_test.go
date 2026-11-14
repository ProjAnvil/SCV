package env

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeTool writes an executable fake tool: a /bin/sh script at path
// (parent directories are created) whose body is script. Discovery
// probes these fakes instead of the host machine's real tools.
func writeTool(t *testing.T, path, script string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("write tool %s: %v", path, err)
	}
	return path
}

// isolate returns EnvVars that pin discovery to test fixtures: PATH and
// HOME resolve only inside the tempdirs, and every environment override
// discovery consults is neutralized, so no assertion can pass merely
// because the host machine happens to have a real tool (or JAVA_HOME, or
// SCV_* vars) set. extra entries are appended last and therefore win.
func isolate(bin, home string, extra ...string) []string {
	vars := []string{
		"PATH=" + bin,
		"HOME=" + home,
		"JAVA_HOME=",
		"SCV_HOME=",
		"SCV_UV=",
		"SCV_PERCEPTION_ROOT=",
		"SCV_PERCEPTION_DIST=",
	}
	return append(vars, extra...)
}

func TestDiscoverFindsTools(t *testing.T) {
	bin := t.TempDir()
	home := t.TempDir()
	scvHome := t.TempDir()
	jvm := filepath.Join(home, "jdk")

	writeTool(t, filepath.Join(bin, "uv"), "echo 'uv 0.9.9'")
	writeTool(t, filepath.Join(bin, "python3"), "exit 0")
	writeTool(t, filepath.Join(bin, "git"), "exit 0")
	writeTool(t, filepath.Join(bin, "go"), "echo 'go version go1.27.0 darwin/arm64'")
	writeTool(t, filepath.Join(jvm, "bin", "java"), "exit 0")
	// A succeeding fake helper keeps the real /usr/libexec/java_home out
	// of the chain on macOS dev machines. It is preempted anyway: the
	// JAVA_HOME below resolves java first.
	writeTool(t, filepath.Join(bin, "java_home"), "echo /absent-helper-home")

	env, err := Discover(Options{
		ScvHome:   scvHome,
		ExtraPath: []string{bin},
		EnvVars:   isolate("", home, "JAVA_HOME="+jvm),
	})
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}

	for name, tool := range map[string]*Tool{
		"uv":      env.Uv,
		"python3": env.Python3,
		"git":     env.Git,
		"go":      env.Go,
		"java":    env.Java,
	} {
		if tool == nil || !tool.OK {
			t.Errorf("%s = %+v, want found and OK", name, tool)
		}
	}
	if got := env.Uv.Path; got != filepath.Join(bin, "uv") {
		t.Errorf("Uv.Path = %q, want %q", got, filepath.Join(bin, "uv"))
	}
	if got := env.Uv.Version; got != "uv 0.9.9" {
		t.Errorf("Uv.Version = %q, want first line of `uv --version` output", got)
	}
	if got := env.Go.Version; got != "go1.27.0" {
		t.Errorf("Go.Version = %q, want %q", got, "go1.27.0")
	}
	if got := env.Java.Path; got != filepath.Join(jvm, "bin", "java") {
		t.Errorf("Java.Path = %q, want %q (JAVA_HOME wins the chain)", got, filepath.Join(jvm, "bin", "java"))
	}

	if env.ScvHome != scvHome {
		t.Errorf("ScvHome = %q, want %q", env.ScvHome, scvHome)
	}
	if want := filepath.Join(scvHome, "perception"); env.PerceptionRoot != want {
		t.Errorf("PerceptionRoot = %q, want %q", env.PerceptionRoot, want)
	}
	if want := filepath.Join(scvHome, "perception", "dist"); env.DistRoot != want {
		t.Errorf("DistRoot = %q, want %q", env.DistRoot, want)
	}
	if env.PrebuiltLoader || env.PrebuiltGraphMCP {
		t.Errorf("PrebuiltLoader/PrebuiltGraphMCP = %t/%t, want false/false (no dist laid out)", env.PrebuiltLoader, env.PrebuiltGraphMCP)
	}
}

func TestDiscoverUvFallbackPaths(t *testing.T) {
	t.Run("SCV_UV override wins", func(t *testing.T) {
		home := t.TempDir()
		uvBin := writeTool(t, filepath.Join(t.TempDir(), "uv"), "echo 'uv 0.9.9'")

		env, err := Discover(Options{EnvVars: isolate("", home, "SCV_UV="+uvBin)})
		if err != nil {
			t.Fatalf("Discover returned error: %v", err)
		}
		if env.Uv == nil || env.Uv.Path != uvBin {
			t.Errorf("Uv = %+v, want path %q from SCV_UV", env.Uv, uvBin)
		}
	})

	t.Run("PATH lookup via EnvVars", func(t *testing.T) {
		bin := t.TempDir()
		home := t.TempDir()
		writeTool(t, filepath.Join(bin, "uv"), "echo 'uv 0.9.9'")

		env, err := Discover(Options{EnvVars: isolate(bin, home)})
		if err != nil {
			t.Fatalf("Discover returned error: %v", err)
		}
		if env.Uv == nil || env.Uv.Path != filepath.Join(bin, "uv") {
			t.Errorf("Uv = %+v, want path under %q", env.Uv, bin)
		}
	})

	t.Run("home fallback when PATH has no uv", func(t *testing.T) {
		home := t.TempDir()
		localBin := writeTool(t, filepath.Join(home, ".local", "bin", "uv"), "echo 'uv 0.9.9'")

		env, err := Discover(Options{EnvVars: isolate("", home)})
		if err != nil {
			t.Fatalf("Discover returned error: %v", err)
		}
		if env.Uv == nil || env.Uv.Path != localBin {
			t.Errorf("Uv = %+v, want fallback path %q", env.Uv, localBin)
		}
	})

	t.Run("candidates keep the spec section 9 order", func(t *testing.T) {
		bin := t.TempDir()
		home := t.TempDir()
		writeTool(t, filepath.Join(bin, "uv"), "exit 0")

		d, err := newDiscoverer(Options{EnvVars: isolate(bin, home, "SCV_UV=/override/uv")})
		if err != nil {
			t.Fatalf("newDiscoverer returned error: %v", err)
		}
		want := []string{
			"/override/uv",
			filepath.Join(bin, "uv"),
			filepath.Join(home, ".local", "bin", "uv"),
			"/opt/homebrew/bin/uv",
		}
		if got := d.uvCandidates(); !slices.Equal(got, want) {
			t.Errorf("uvCandidates() = %v, want %v", got, want)
		}
	})

	t.Run("non-executable SCV_UV falls through to PATH", func(t *testing.T) {
		bin := t.TempDir()
		home := t.TempDir()
		dead := filepath.Join(home, "uv")
		if err := os.WriteFile(dead, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
			t.Fatalf("write non-executable uv: %v", err)
		}
		writeTool(t, filepath.Join(bin, "uv"), "echo 'uv 0.9.9'")

		env, err := Discover(Options{EnvVars: isolate(bin, home, "SCV_UV="+dead)})
		if err != nil {
			t.Fatalf("Discover returned error: %v", err)
		}
		if env.Uv == nil || env.Uv.Path != filepath.Join(bin, "uv") {
			t.Errorf("Uv = %+v, want PATH hit after skipping non-executable SCV_UV", env.Uv)
		}
	})

	t.Run("probe failure leaves version empty but tool OK", func(t *testing.T) {
		home := t.TempDir()
		uvBin := writeTool(t, filepath.Join(home, "bin", "uv"), "exit 1")

		env, err := Discover(Options{EnvVars: isolate("", home, "SCV_UV="+uvBin)})
		if err != nil {
			t.Fatalf("Discover returned error: %v", err)
		}
		if env.Uv == nil || !env.Uv.OK {
			t.Fatalf("Uv = %+v, want found and OK despite failed probe", env.Uv)
		}
		if env.Uv.Version != "" {
			t.Errorf("Uv.Version = %q, want empty on probe failure", env.Uv.Version)
		}
	})
}

func TestDiscoverPrebuiltDist(t *testing.T) {
	layout := func(t *testing.T, paths ...string) string {
		t.Helper()
		root := t.TempDir()
		for _, rel := range paths {
			writeTool(t, filepath.Join(root, filepath.FromSlash(rel)), "exit 0")
		}
		return root
	}

	t.Run("both prebuilt binaries", func(t *testing.T) {
		scvHome := layout(t, "perception/dist/loader/perception-loader", "perception/dist/graphmcp/scv-graph-mcp")

		env, err := Discover(Options{ScvHome: scvHome, EnvVars: isolate("", t.TempDir())})
		if err != nil {
			t.Fatalf("Discover returned error: %v", err)
		}
		if !env.PrebuiltLoader || !env.PrebuiltGraphMCP {
			t.Errorf("Prebuilt = %t/%t, want true/true", env.PrebuiltLoader, env.PrebuiltGraphMCP)
		}
		if want := filepath.Join(scvHome, "perception", "dist"); env.DistRoot != want {
			t.Errorf("DistRoot = %q, want %q", env.DistRoot, want)
		}
	})

	t.Run("loader only", func(t *testing.T) {
		scvHome := layout(t, "perception/dist/loader/perception-loader")

		env, err := Discover(Options{ScvHome: scvHome, EnvVars: isolate("", t.TempDir())})
		if err != nil {
			t.Fatalf("Discover returned error: %v", err)
		}
		if !env.PrebuiltLoader || env.PrebuiltGraphMCP {
			t.Errorf("Prebuilt = %t/%t, want true/false", env.PrebuiltLoader, env.PrebuiltGraphMCP)
		}
	})

	t.Run("SCV_PERCEPTION_DIST overrides the default dist dir", func(t *testing.T) {
		scvHome := t.TempDir()
		dist := layout(t, "loader/perception-loader")

		env, err := Discover(Options{ScvHome: scvHome, EnvVars: isolate("", t.TempDir(), "SCV_PERCEPTION_DIST="+dist)})
		if err != nil {
			t.Fatalf("Discover returned error: %v", err)
		}
		if env.DistRoot != dist {
			t.Errorf("DistRoot = %q, want override %q", env.DistRoot, dist)
		}
		if !env.PrebuiltLoader {
			t.Error("PrebuiltLoader = false, want true from the override dist")
		}
	})

	t.Run("SCV_PERCEPTION_ROOT redirects the default dist", func(t *testing.T) {
		root := layout(t, "dist/loader/perception-loader")
		home := t.TempDir()

		env, err := Discover(Options{EnvVars: isolate("", home, "SCV_PERCEPTION_ROOT="+root)})
		if err != nil {
			t.Fatalf("Discover returned error: %v", err)
		}
		if env.PerceptionRoot != root {
			t.Errorf("PerceptionRoot = %q, want %q", env.PerceptionRoot, root)
		}
		if want := filepath.Join(root, "dist"); env.DistRoot != want {
			t.Errorf("DistRoot = %q, want %q", env.DistRoot, want)
		}
		if !env.PrebuiltLoader {
			t.Error("PrebuiltLoader = false, want true under the redirected root")
		}
	})
}

func TestDiscoverGoVersion(t *testing.T) {
	cases := []struct {
		name   string
		script string
		want   string
		wantOK bool
	}{
		{"meets minimum", "echo 'go version go1.27.0 darwin/arm64'", "go1.27.0", true},
		{"above minimum", "echo 'go version go1.28.3 linux/amd64'", "go1.28.3", true},
		{"newer major", "echo 'go version go2.1.0 linux/amd64'", "go2.1.0", true},
		{"below minimum", "echo 'go version go1.21.5 darwin/arm64'", "go1.21.5", false},
		{"unparseable output", "echo 'go version go1 abc go1.x darwin/arm64'", "go version go1 abc go1.x darwin/arm64", false},
		{"garbage output", "echo 'this go binary speaks only riddles'", "this go binary speaks only riddles", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			writeTool(t, filepath.Join(bin, "go"), tc.script)

			env, err := Discover(Options{ExtraPath: []string{bin}, EnvVars: isolate("", t.TempDir())})
			if err != nil {
				t.Fatalf("Discover returned error: %v", err)
			}
			if env.Go == nil {
				t.Fatal("Go = nil, want found")
			}
			if env.Go.OK != tc.wantOK {
				t.Errorf("Go.OK = %t, want %t", env.Go.OK, tc.wantOK)
			}
			if env.Go.Version != tc.want {
				t.Errorf("Go.Version = %q, want %q", env.Go.Version, tc.want)
			}
		})
	}
}

func TestDiscoverJavaChain(t *testing.T) {
	// helperWithoutJava points the java_home helper step at a directory
	// that has no bin/java, so the chain deterministically continues past
	// it (and never reaches the real /usr/libexec/java_home).
	helperWithoutJava := func(t *testing.T, bin string) string {
		t.Helper()
		emptyHome := t.TempDir()
		writeTool(t, filepath.Join(bin, "java_home"), "echo "+emptyHome)
		return emptyHome
	}

	t.Run("JAVA_HOME wins the chain", func(t *testing.T) {
		bin := t.TempDir()
		home := t.TempDir()
		jvm := filepath.Join(home, "jdk")
		writeTool(t, filepath.Join(jvm, "bin", "java"), "exit 0")
		helperWithoutJava(t, bin)

		env, err := Discover(Options{ExtraPath: []string{bin}, EnvVars: isolate("", home, "JAVA_HOME="+jvm)})
		if err != nil {
			t.Fatalf("Discover returned error: %v", err)
		}
		if env.Java == nil || env.Java.Path != filepath.Join(jvm, "bin", "java") {
			t.Errorf("Java = %+v, want %q", env.Java, filepath.Join(jvm, "bin", "java"))
		}
	})

	t.Run("java_home helper output", func(t *testing.T) {
		bin := t.TempDir()
		home := t.TempDir()
		jvm := filepath.Join(home, "helper-jdk")
		writeTool(t, filepath.Join(jvm, "bin", "java"), "exit 0")
		writeTool(t, filepath.Join(bin, "java_home"), "echo "+jvm)

		env, err := Discover(Options{ExtraPath: []string{bin}, EnvVars: isolate("", home)})
		if err != nil {
			t.Fatalf("Discover returned error: %v", err)
		}
		if env.Java == nil || env.Java.Path != filepath.Join(jvm, "bin", "java") {
			t.Errorf("Java = %+v, want %q from the helper's home", env.Java, filepath.Join(jvm, "bin", "java"))
		}
	})

	t.Run("sdkman current candidate", func(t *testing.T) {
		bin := t.TempDir()
		home := t.TempDir()
		helperWithoutJava(t, bin)
		sdkman := writeTool(t, filepath.Join(home, ".sdkman", "candidates", "java", "current", "bin", "java"), "exit 0")

		env, err := Discover(Options{ExtraPath: []string{bin}, EnvVars: isolate("", home)})
		if err != nil {
			t.Fatalf("Discover returned error: %v", err)
		}
		if env.Java == nil || env.Java.Path != sdkman {
			t.Errorf("Java = %+v, want %q", env.Java, sdkman)
		}
	})

	t.Run("JavaVirtualMachines directory scan", func(t *testing.T) {
		bin := t.TempDir()
		home := t.TempDir()
		helperWithoutJava(t, bin)
		scanned := writeTool(t, filepath.Join(home, "Library", "Java", "JavaVirtualMachines", "temurin-21", "Contents", "Home", "bin", "java"), "exit 0")

		env, err := Discover(Options{ExtraPath: []string{bin}, EnvVars: isolate("", home)})
		if err != nil {
			t.Fatalf("Discover returned error: %v", err)
		}
		if env.Java == nil || env.Java.Path != scanned {
			t.Errorf("Java = %+v, want %q from the directory scan", env.Java, scanned)
		}
	})

	t.Run("absent everywhere yields nil, never an error", func(t *testing.T) {
		bin := t.TempDir()
		home := t.TempDir()
		helperWithoutJava(t, bin)

		env, err := Discover(Options{ExtraPath: []string{bin}, EnvVars: isolate("", home)})
		if err != nil {
			t.Fatalf("Discover returned error: %v, want nil (java is informational)", err)
		}
		if env.Java != nil {
			t.Errorf("Java = %+v, want nil", env.Java)
		}
	})
}

func TestDiscoverMissingToolsIsolated(t *testing.T) {
	bin := t.TempDir()
	home := t.TempDir()
	// A failing java_home fake keeps the real macOS helper from being the
	// deciding factor, and a directory named like a tool must be skipped
	// by the PATH lookup.
	writeTool(t, filepath.Join(bin, "java_home"), "echo "+filepath.Join(t.TempDir(), "no-java-here"))
	if err := os.MkdirAll(filepath.Join(bin, "git"), 0o755); err != nil {
		t.Fatalf("mkdir fake git directory: %v", err)
	}

	// PATH="::" keeps isolation and also exercises the empty-segment
	// skip in the PATH scan (degenerate but legal PATH syntax).
	env, err := Discover(Options{ExtraPath: []string{bin}, EnvVars: isolate("::", home, "JAVA_HOME=/absent-jvm")})
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	// Uv is deliberately not asserted here: its candidate chain ends with
	// the absolute /opt/homebrew/bin/uv fallback, which is host-dependent
	// by design. Uv-missing semantics are covered by the gate tests.
	if env.Python3 != nil {
		t.Errorf("Python3 = %+v, want nil (PATH is isolated)", env.Python3)
	}
	if env.Git != nil {
		t.Errorf("Git = %+v, want nil (directories on PATH are not tools)", env.Git)
	}
	if env.Go != nil {
		t.Errorf("Go = %+v, want nil", env.Go)
	}
	if env.Java != nil {
		t.Errorf("Java = %+v, want nil (chain is isolated)", env.Java)
	}
}

func TestDiscoverRoots(t *testing.T) {
	t.Run("explicit ScvHome wins over SCV_HOME", func(t *testing.T) {
		env, err := Discover(Options{ScvHome: "/explicit/scv", EnvVars: isolate("", t.TempDir(), "SCV_HOME=/env/scv")})
		if err != nil {
			t.Fatalf("Discover returned error: %v", err)
		}
		if env.ScvHome != "/explicit/scv" {
			t.Errorf("ScvHome = %q, want /explicit/scv", env.ScvHome)
		}
	})

	t.Run("SCV_HOME env when no explicit option", func(t *testing.T) {
		env, err := Discover(Options{EnvVars: isolate("", t.TempDir(), "SCV_HOME=/env/scv")})
		if err != nil {
			t.Fatalf("Discover returned error: %v", err)
		}
		if env.ScvHome != "/env/scv" {
			t.Errorf("ScvHome = %q, want /env/scv", env.ScvHome)
		}
	})

	t.Run("default is HOME/.scv", func(t *testing.T) {
		home := t.TempDir()
		env, err := Discover(Options{EnvVars: isolate("", home)})
		if err != nil {
			t.Fatalf("Discover returned error: %v", err)
		}
		if want := filepath.Join(home, ".scv"); env.ScvHome != want {
			t.Errorf("ScvHome = %q, want %q", env.ScvHome, want)
		}
	})
}

func TestDiscoverHomeUnresolvable(t *testing.T) {
	// HOME= (explicitly empty) is authoritative, and a malformed entry is
	// ignored rather than aborting the parse.
	_, err := Discover(Options{EnvVars: []string{"PATH=", "HOME=", "NOT_AN_ASSIGNMENT"}})
	if err == nil {
		t.Fatal("Discover returned nil error, want error for unresolvable home directory")
	}
}
