#!/usr/bin/env bash
# Smoke test for scripts/pack.sh (Task 11). Guarded by SCV_PACK_TEST=1:
# the test compiles real Go binaries, so it never runs as part of
# `make engine-test`; run it explicitly on a dev machine with
#
#   SCV_PACK_TEST=1 bash scripts/pack_test.sh
#
# To keep it fast it overrides SCV_PACK_PLATFORMS with just the current
# platform (pack.sh's platform-list knob, default: the four
# linux/darwin x amd64/arm64 pairs) and SCV_PACK_OUT with a tempdir.
set -euo pipefail

repo_root=$(cd "$(dirname "$0")/.." && pwd)

if [ "${SCV_PACK_TEST:-}" != "1" ]; then
  echo "skipped: set SCV_PACK_TEST=1 to run the pack smoke test"
  exit 0
fi

fail() { echo "FAIL: $*" >&2; exit 1; }

# One platform only: the host's own, so the go builds stay native and fast.
platform="$(go env GOOS)-$(go env GOARCH)"
os=${platform%-*}
arch=${platform#*-}
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

SCV_PACK_PLATFORMS="$platform" SCV_PACK_OUT="$out" bash "$repo_root/scripts/pack.sh" \
  || fail "pack.sh exited nonzero"

ver=$(sed -n 's/^## \[\(v[0-9]*\.[0-9]*\.[0-9]*\)\].*/\1/p' "$repo_root/CHANGELOG.md" | head -1)
tarball="$out/perception-runtime-$ver.tar.gz"
[ -f "$tarball" ] || fail "runtime tarball missing: $tarball"
[ -x "$out/scv-$ver-$os-$arch" ] || fail "engine binary missing: scv-$ver-$os-$arch"

# TestPackWhitelist (plan-pinned): the whitelist lives in the listing,
# not in excludes.
listing=$(tar -tzf "$tarball")

check_has() { grep -qx "$1" <<<"$listing" || fail "tarball missing whitelisted entry: $1"; }
check_not() { if grep -qx "$1" <<<"$listing"; then fail "tarball contains non-whitelisted entry: $1"; fi; }

# Spec section 11 whitelist, spot-checked entry by entry.
check_has "perceive.py"
check_has "analyzer/analyzer.py"
check_has "analyzer/uv.lock"
check_has "loader/go.mod"
check_has "graphmcp/main.go"
check_has "adapters/springboot/MANIFEST.json"
check_has "docgen/en/project-analyzer.md"
check_has "docgen/zh-cn/project-analyzer.md"
check_has "docgen/en/SUMMARY.template.md"
check_has "docgen/zh-cn/ARCHITECTURE.template.md"
check_has "dist/$platform/loader/perception-loader"
check_has "dist/$platform/graphmcp/scv-graph-mcp"

# Exclusion by construction: the dev checkout HAS analyzer/.venv and
# adapter sources, none of which may enter the tarball.
if grep -q '\.venv' <<<"$listing"; then fail "tarball contains .venv"; fi
if grep -q '__pycache__' <<<"$listing"; then fail "tarball contains __pycache__"; fi
check_not "adapters/springboot/src"
check_not "adapters/springboot/README.md"
check_not "adapters/springboot/build.gradle"

# The prebuilt pair is a real executable, not a zero-byte stub.
[ -s "$out/scv-$ver-$os-$arch" ] || fail "engine binary is empty"

# pack.sh also writes the in-checkout docgen mirror so a dev checkout
# satisfies docgen asset resolution without unpacking the tarball.
[ -f "$repo_root/perception/docgen/en/project-analyzer.md" ] \
  || fail "in-checkout docgen mirror missing: perception/docgen/en/project-analyzer.md"
[ -f "$repo_root/perception/docgen/zh-cn/SUMMARY.template.md" ] \
  || fail "in-checkout docgen mirror missing: perception/docgen/zh-cn/SUMMARY.template.md"
if diff -q "$repo_root/agents/en/project-analyzer.md" \
  "$repo_root/perception/docgen/en/project-analyzer.md" >/dev/null; then
  :
else
  fail "docgen mirror diverges from agents/en/project-analyzer.md"
fi

echo "PASS: pack smoke test (ver=$ver platform=$platform)"
