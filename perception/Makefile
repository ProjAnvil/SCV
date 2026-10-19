# Perception developer entrypoints (repo/test-time only — nothing here ships
# to user machines; install.sh whitelists perceive.py/loader/graphmcp/analyzer).

.PHONY: test crosscheck fixture-list fixture-run fixture-verify fixture-clean

# ---- fixture harness ---------------------------------------------------- #
# Deterministic end-to-end exercise of perceive.py over the checked-in
# multi-language fixtures in ./fixture. This mirrors what `scv run` Step 5.5
# does (skills/*/references/run.md), minus the LLM-driven document tiers:
#
#     python3 perceive.py <repo> <analysis_dir> --source local --lang en
#
# Paths are relative to this directory, so the harness never touches
# ~/.scv/analysis and can be run from a clean checkout.

REPOS_HOME    ?= fixture/repos
ANALYSIS_HOME ?= fixture/analysis
REPO          ?=
EXTENSIONS    ?= springboot
PYTHON        ?= python3
FIXTURE_DIR   ?= fixture

# The recipes below are Python heredocs, so these must reach the environment.
export REPOS_HOME ANALYSIS_HOME REPO EXTENSIONS FIXTURE_DIR

# Structure mode (empty extension list) needs its own config so the run does not
# depend on whatever ~/.scv/config.json happens to say.
STRUCTURE_CFG := $(FIXTURE_DIR)/config.structure.json
SPRING_CFG    := $(FIXTURE_DIR)/config.springboot.json

# Every v2 suite: analyzer (uv + pytest), orchestrator unit/integration +
# query matrix (python3 + unittest), and the Go tier (loader + graphmcp).
test:
	cd analyzer && uv run pytest
	python3 -m unittest discover -s tests
	go test -C loader ./...
	go test -C graphmcp ./...

# Cross-validation: the reference (Spoon) engine vs the tree-sitter analyzer
# over the 5 frozen fixtures, diffed through the spec §5 multiset projection.
# Needs java + gradle + uv on PATH; see tools/crosscheck.py.
crosscheck:
	python3 tools/crosscheck.py

# List the fixture repositories that the harness would analyse.
fixture-list:
	@for d in $(REPOS_HOME)/*/; do \
		[ -d "$$d" ] || continue; \
		echo "  $$(basename $$d)"; \
	done

# Analyse every fixture (or just $(REPO)) into $(ANALYSIS_HOME)/<repo>.
# EXTENSIONS=springboot runs the component tier; EXTENSIONS= (empty) runs
# structure-only mode, which is the only way plain-Java repos are analysed.
# Analyse every fixture (or just $(REPO)) into $(ANALYSIS_HOME)/<repo>.
#   make fixture-run                       # Spring Boot component tier
#   make fixture-run EXTENSIONS=           # structure-only mode (plain Java)
#   make fixture-run REPO=java-plain-core EXTENSIONS=
fixture-run:
	$(PYTHON) fixture/run_fixtures.py --repos $(REPOS_HOME) \
		--analysis $(ANALYSIS_HOME) --extensions "$(EXTENSIONS)" \
		$(if $(REPO),--repo $(REPO),)

# Assert the frozen expectations: which fixtures must produce structure, which
# must skip, and which cycles must be graded fatal vs tolerated.
fixture-verify:
	$(PYTHON) fixture/run_fixtures.py --repos $(REPOS_HOME) \
		--analysis $(ANALYSIS_HOME) --extensions "$(EXTENSIONS)" \
		$(if $(REPO),--repo $(REPO),)
	$(PYTHON) fixture/verify_fixtures.py --analysis $(ANALYSIS_HOME)

# Wipe generated analysis output (the fixture sources are never touched).
fixture-clean:
	rm -rf $(ANALYSIS_HOME)/*
	rm -f $(STRUCTURE_CFG) $(SPRING_CFG)
