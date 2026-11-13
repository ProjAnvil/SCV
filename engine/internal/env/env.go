// Package env discovers the external tools and filesystem roots the SCV
// engine depends on (spec section 9) and hands the snapshot to the
// unified dependency gate (gate.go, spec section 7).
//
// Discovery is fully injectable: Options.ExtraPath prepends directories
// to the PATH search and Options.EnvVars overrides environment variables,
// so tests run against tempdir fakes instead of the host machine's tools.
package env

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Tool is a discovered external tool. OK reports whether the tool is
// usable: found on disk and, for version-constrained tools such as Go,
// meeting the engine's minimum version.
type Tool struct {
	Path    string
	Version string
	OK      bool
}

// Env is the environment snapshot shared by `scv env` and the checking
// gate. A nil Tool means the tool was not found; the Prebuilt flags
// report the prebuilt dist binaries that substitute for a Go toolchain
// (spec section 12, binary-OR-toolchain gate).
type Env struct {
	ScvHome        string
	PerceptionRoot string
	DistRoot       string

	Uv      *Tool
	Python3 *Tool
	Git     *Tool
	Go      *Tool
	Java    *Tool

	PrebuiltLoader   bool
	PrebuiltGraphMCP bool
}

// Options parameterizes Discover. ScvHome is the `--scv-home` override;
// ExtraPath entries are searched before every PATH directory; EnvVars
// entries ("KEY=VALUE", later entries win) override the process
// environment, letting tests pin PATH and HOME to tempdirs.
type Options struct {
	ScvHome   string
	ExtraPath []string
	EnvVars   []string
}

// macOSJavaHomeHelper is the macOS JVM resolver from spec section 9.
const macOSJavaHomeHelper = "/usr/libexec/java_home"

// discoverer carries the resolved environment for one Discover call.
type discoverer struct {
	env      map[string]string
	home     string
	pathDirs []string
}

// newDiscoverer resolves the effective environment: the process
// environment overlaid with the Options.EnvVars overrides, the home
// directory, and the PATH search order (ExtraPath first, then PATH).
func newDiscoverer(opts Options) (*discoverer, error) {
	env := make(map[string]string, len(os.Environ())+len(opts.EnvVars))
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	for _, kv := range opts.EnvVars {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		env[k] = v
	}
	// Home resolution is authoritative rather than falling back to the
	// process home: an explicitly empty HOME (test isolation or a broken
	// shell) is an error, because every fallback path is home-relative
	// and silently using the wrong home would misreport the snapshot.
	home := env["HOME"]
	if home == "" {
		return nil, fmt.Errorf("env: cannot resolve home directory: HOME is empty")
	}
	d := &discoverer{
		env:      env,
		home:     home,
		pathDirs: append(slices.Clone(opts.ExtraPath), filepath.SplitList(env["PATH"])...),
	}
	return d, nil
}

func (d *discoverer) envGet(key string) string {
	return d.env[key]
}

// dirCandidates joins name onto every search directory (ExtraPath first,
// then PATH); empty PATH entries are skipped.
func (d *discoverer) dirCandidates(name string) []string {
	var candidates []string
	for _, dir := range d.pathDirs {
		if dir == "" {
			continue
		}
		candidates = append(candidates, filepath.Join(dir, name))
	}
	return candidates
}

// lookPath returns the first executable match for name across ExtraPath
// and PATH directories (ExtraPath first).
func (d *discoverer) lookPath(name string) string {
	return firstExecutable(d.dirCandidates(name))
}

// firstExecutable returns the first candidate that is an executable
// file, or "" when none resolves.
func firstExecutable(candidates []string) string {
	for _, path := range candidates {
		if isExecutableFile(path) {
			return path
		}
	}
	return ""
}

// probeTool wraps a resolved path in an OK Tool, setting Version to the
// first line of `path probeArgs...` output ("" when the probe yields
// nothing — a tool found but unable to report its version stays OK). An
// empty path yields a nil Tool.
func probeTool(path string, probeArgs ...string) *Tool {
	if path == "" {
		return nil
	}
	return &Tool{Path: path, OK: true, Version: probeOutput(path, probeArgs...)}
}

// probeOutput runs path with args and returns the first line of stdout,
// trimmed, or "" on any failure. Version probes are best-effort.
func probeOutput(path string, args ...string) string {
	out, err := exec.Command(path, args...).Output()
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(line)
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// Discover runs the full environment probe (spec section 9): roots
// first, then tools, then the prebuilt dist binaries. Only an
// unresolvable home directory is an error; every other miss degrades
// into a nil Tool or false Prebuilt flag for the gate to classify —
// discovery never fails because something is merely missing.
func Discover(opts Options) (*Env, error) {
	d, err := newDiscoverer(opts)
	if err != nil {
		return nil, err
	}

	scvHome := opts.ScvHome
	if scvHome == "" {
		if fromEnv := d.envGet("SCV_HOME"); fromEnv != "" {
			scvHome = fromEnv
		} else {
			scvHome = filepath.Join(d.home, ".scv")
		}
	}
	perceptionRoot := d.envGet("SCV_PERCEPTION_ROOT")
	if perceptionRoot == "" {
		perceptionRoot = filepath.Join(scvHome, "perception")
	}
	distRoot := d.envGet("SCV_PERCEPTION_DIST")
	if distRoot == "" {
		distRoot = filepath.Join(perceptionRoot, "dist")
	}

	return &Env{
		ScvHome:        scvHome,
		PerceptionRoot: perceptionRoot,
		DistRoot:       distRoot,
		Uv:             d.findUV(),
		Python3:        probeTool(d.lookPath("python3")),
		Git:            probeTool(d.lookPath("git")),
		Go:             d.findGo(),
		Java:           d.findJava(),
		// Mtime freshness is deliberately not checked here; perceive.py
		// owns staleness and rebuild decisions (spec section 10).
		PrebuiltLoader:   fileExists(filepath.Join(distRoot, "loader", "perception-loader")),
		PrebuiltGraphMCP: fileExists(filepath.Join(distRoot, "graphmcp", "scv-graph-mcp")),
	}, nil
}

// uvCandidates lists the uv resolution candidates in spec section 9
// order: $SCV_UV, PATH (ExtraPath first), ~/.local/bin/uv, then
// /opt/homebrew/bin/uv.
func (d *discoverer) uvCandidates() []string {
	candidates := []string{
		filepath.Join(d.home, ".local", "bin", "uv"),
		"/opt/homebrew/bin/uv",
	}
	if pathHit := d.lookPath("uv"); pathHit != "" {
		candidates = append([]string{pathHit}, candidates...)
	}
	if fromEnv := d.envGet("SCV_UV"); fromEnv != "" {
		candidates = append([]string{fromEnv}, candidates...)
	}
	return candidates
}

func (d *discoverer) findUV() *Tool {
	return probeTool(firstExecutable(d.uvCandidates()), "--version")
}

// goMinimumMinor is the minimum Go toolchain the dist build supports
// (spec section 9: Go >= 1.27).
const goMinimumMinor = 27

func (d *discoverer) findGo() *Tool {
	return goTool(probeTool(d.lookPath("go"), "version"))
}

// goTool applies the version floor to a probed Go Tool; a nil Tool stays
// nil.
func goTool(t *Tool) *Tool {
	if t == nil {
		return nil
	}
	version, meets := parseGoVersion(t.Version)
	t.Version = version
	t.OK = meets
	return t
}

// parseGoVersion extracts the goX.Y[.Z] token from `go version` output
// and reports whether it meets the engine minimum. Unparseable output
// yields the raw line and meets=false, so a broken toolchain degrades
// exactly like a missing one.
func parseGoVersion(line string) (version string, meets bool) {
	for _, field := range strings.Fields(line) {
		if len(field) <= 2 || !strings.HasPrefix(field, "go") {
			continue
		}
		segments := strings.Split(strings.TrimPrefix(field, "go"), ".")
		if len(segments) < 2 {
			continue
		}
		major, errMajor := strconv.Atoi(segments[0])
		minor, errMinor := strconv.Atoi(segments[1])
		if errMajor != nil || errMinor != nil {
			continue
		}
		return field, major > 1 || (major == 1 && minor >= goMinimumMinor)
	}
	return line, false
}

// findJava resolves java through the JAVA_HOME chain of spec section 9:
// $JAVA_HOME/bin/java, the java_home helper, sdkman's current candidate,
// then the macOS JavaVirtualMachines directory scan. Java is
// informational only — it never appears in gate results.
func (d *discoverer) findJava() *Tool {
	if home := d.envGet("JAVA_HOME"); home != "" {
		if p := filepath.Join(home, "bin", "java"); isExecutableFile(p) {
			return &Tool{Path: p, OK: true}
		}
	}
	if home := d.runJavaHomeHelper(); home != "" {
		if p := filepath.Join(home, "bin", "java"); isExecutableFile(p) {
			return &Tool{Path: p, OK: true}
		}
	}
	if p := filepath.Join(d.home, ".sdkman", "candidates", "java", "current", "bin", "java"); isExecutableFile(p) {
		return &Tool{Path: p, OK: true}
	}
	// Glob errors only on a malformed pattern; the pattern is constant.
	matches, _ := filepath.Glob(filepath.Join(d.home, "Library", "Java", "JavaVirtualMachines", "*", "Contents", "Home", "bin", "java"))
	for _, p := range matches {
		if isExecutableFile(p) {
			return &Tool{Path: p, OK: true}
		}
	}
	return nil
}

// runJavaHomeHelper runs the java_home helper and returns the JVM home
// it prints, or "" when no helper resolves or it fails. The helper is
// resolved through the PATH lookup first: production machines never
// carry java_home on PATH, so /usr/libexec/java_home stays the effective
// resolver there, while tests substitute a fake via ExtraPath.
func (d *discoverer) runJavaHomeHelper() string {
	if p := d.lookPath("java_home"); p != "" {
		return probeOutput(p)
	}
	return probeOutput(macOSJavaHomeHelper)
}
