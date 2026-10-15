# Perception developer entrypoints (repo/test-time only — nothing here ships
# to user machines; install.sh whitelists perceive.py/loader/graphmcp/analyzer).

.PHONY: test crosscheck

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
