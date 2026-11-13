#!/usr/bin/env bash
# Build the SCV release artifacts (spec section 11, Task 11):
#
#   1. perception-runtime-<ver>.tar.gz — the perception runtime, packed
#      from an EXPLICIT whitelist (exclusion by construction: nothing is
#      copied that is not on this list):
#        perceive.py
#        analyzer/            sources + uv.lock, without .venv/__pycache__
#        loader/              Go sources (own go.mod)
#        graphmcp/            Go sources (own go.mod)
#        adapters/*/MANIFEST.json   manifests only, no adapter sources
#        docgen/{en,zh-cn}/   mirror of agents/<lang>/project-analyzer.md
#                             + skills/<lang>/references/templates/*.template.md
#        dist/<platform>/     prebuilt loader + graphmcp pairs,
#                             CGO_ENABLED=0, linux/darwin x amd64/arm64
#   2. scv-<ver>-<os>-<arch> — the engine binary, same platforms,
#      statically linked (vectra is pure Go, so CGO_ENABLED=0 suffices).
#
# <ver> is the first "## [vX.Y.Z]" heading in CHANGELOG.md (the head
# section is [Unreleased] and is skipped).
#
# docgen mirror disposition: the mirror is GENERATED AT PACK TIME ONLY,
# both into the tarball and in-checkout at perception/docgen/ so a dev
# checkout satisfies docgen asset resolution (spec section 8) without
# unpacking a tarball. perception/docgen/ is never committed — it is
# listed in .gitignore.
#
# Knobs (environment):
#   SCV_PACK_PLATFORMS  space-separated platform ids to build
#                       (default: "linux-amd64 linux-arm64 darwin-amd64
#                       darwin-arm64"; the smoke test in pack_test.sh
#                       overrides this with just the current platform).
#   SCV_PACK_OUT        artifact directory (default: dist-release/,
#                       gitignored).
set -euo pipefail

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

platforms=${SCV_PACK_PLATFORMS:-"linux-amd64 linux-arm64 darwin-amd64 darwin-arm64"}
outdir=${SCV_PACK_OUT:-dist-release}

# Version: first released "## [vX.Y.Z]" heading, skipping [Unreleased].
ver=$(sed -n 's/^## \[\(v[0-9]*\.[0-9]*\.[0-9]*\)\].*/\1/p' CHANGELOG.md | head -1)
if [ -z "$ver" ]; then
  echo "pack.sh: no '## [vX.Y.Z]' heading found in CHANGELOG.md" >&2
  exit 1
fi

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir -p "$outdir"

# mirror_docgen writes the docgen assets mirror into <dest>/<lang>/:
# project-analyzer.md from agents/, the *.template.md prompts flattened
# from skills/<lang>/references/templates/ — the packed layout
# engine/internal/docgen resolves.
mirror_docgen() {
  local dest=$1 lang
  for lang in en zh-cn; do
    rm -rf "$dest/$lang"
    mkdir -p "$dest/$lang"
    cp "agents/$lang/project-analyzer.md" "$dest/$lang/project-analyzer.md"
    cp skills/"$lang"/references/templates/*.template.md "$dest/$lang/"
  done
}

echo "pack: version $ver, platforms: $platforms"

# Stage the whitelisted runtime sources (exclusion by construction:
# every entry below is an explicit whitelist pick).
cp perception/perceive.py "$stage/perceive.py"
for dir in analyzer loader graphmcp; do
  # bsdtar/GNU-tar pipe; the excludes guard the analyzer dev tree
  # (.venv, __pycache__) and stray .DS_Store from entering the copy.
  tar -C perception \
    --exclude='.venv' --exclude='.venv/*' \
    --exclude='__pycache__' --exclude='__pycache__/*' \
    --exclude='.DS_Store' \
    -cf - "$dir" | tar -C "$stage" -xf -
done
for manifest in perception/adapters/*/MANIFEST.json; do
  adapter=$(basename "$(dirname "$manifest")")
  mkdir -p "$stage/adapters/$adapter"
  cp "$manifest" "$stage/adapters/$adapter/MANIFEST.json"
done
mirror_docgen "$stage/docgen"

# Prebuilt loader/graphmcp pairs and the engine binaries, one per
# platform. -o paths are absolute so `go build -C` directory changes
# cannot reinterpret them.
stage_abs=$(cd "$stage" && pwd)
outdir_abs=$(cd "$outdir" && pwd)
for platform in $platforms; do
  os=${platform%-*}
  arch=${platform#*-}
  mkdir -p "$stage_abs/dist/$platform/loader" "$stage_abs/dist/$platform/graphmcp"
  echo "pack: building dist/$platform pair"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -C perception/loader -o "$stage_abs/dist/$platform/loader/perception-loader" .
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -C perception/graphmcp -o "$stage_abs/dist/$platform/graphmcp/scv-graph-mcp" .
  echo "pack: building engine scv-$ver-$os-$arch"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -C engine -o "$outdir_abs/scv-$ver-$os-$arch" ./cmd/scv
done

# The runtime tarball is cut from the staged whitelist (explicit
# top-level list again — nothing else in $stage ships).
tar -C "$stage" -czf "$outdir_abs/perception-runtime-$ver.tar.gz" \
  perceive.py analyzer loader graphmcp adapters docgen dist

# In-checkout docgen mirror (see disposition note above).
mirror_docgen perception/docgen

echo "pack: wrote $outdir/perception-runtime-$ver.tar.gz and engine binaries in $outdir/"
