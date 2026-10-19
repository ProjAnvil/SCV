# Perception developer entrypoints (repo/test-time only — nothing here ships
# to user machines; install.sh whitelists perceive.py/loader/graphmcp/analyzer).

.PHONY: test crosscheck

# ---- fixture harness ---------------------------------------------------- #
# Sources live in ./fixture/repos; generated output lands in ./fixture/analysis
# (gitignored). Nothing is written outside this directory, so the harness never
# touches ~/.scv/analysis or ~/.scv/config.json.

REPOS_HOME    ?= fixture/repos
ANALYSIS_HOME ?= fixture/analysis
EXTENSIONS    ?= springboot
PYTHON        ?= python3

# Every v2 suite: analyzer (uv + pytest), orchestrator unit/integration +
# query matrix (python3 + unittest), the Go tier (loader + graphmcp), and the
# end-to-end fixture check over ./fixture/repos.
#
#   make test                  # Spring Boot component tier (default)
#   make test EXTENSIONS=      # structure-only mode: File/Type/Method layer
#                              # only, which is the only way a non-Spring Java
#                              # repo is analysed at all
test:
	cd analyzer && uv run pytest
	python3 -m unittest discover -s tests
	go test -C loader ./...
	go test -C graphmcp ./...
	$(PYTHON) fixture/check.py --repos $(REPOS_HOME) \
		--analysis $(ANALYSIS_HOME) --extensions "$(EXTENSIONS)"

# Cross-validation: the reference (Spoon) engine vs the tree-sitter analyzer
# over the 5 frozen fixtures, diffed through the spec §5 multiset projection.
# Needs java + gradle + uv on PATH; see tools/crosscheck.py.
crosscheck:
	python3 tools/crosscheck.py
