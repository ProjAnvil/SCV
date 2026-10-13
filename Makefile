# SCV eval — smelt-powered skill evaluation. Details: eval/README.md
#
#   make eval-sync && make eval-smoke     # setup + credential check
#   make eval-run                         # full evaluation (behavior + writing + lint + challenge)
#   make eval-lint                        # static lint only — free, no LLM calls
#   make eval-compare                     # diff two saved runs (see COMPARE_LANG/OLD_TAG/TAG)

EVAL_DIR      := eval
TAG           ?= optimized
TIMES         ?= 2
LANGS         ?= all
COMPARE_LANG  ?= en
OLD_TAG       ?= baseline

.PHONY: eval-sync eval-smoke eval-lint eval-run eval-compare eval-clean

eval-sync: ## install eval deps (smelt via path from ../skillcheck)
	cd $(EVAL_DIR) && uv sync

eval-smoke: ## one cheap LLM call to verify eval/.env credentials
	cd $(EVAL_DIR) && uv run python smoke_env.py

eval-lint: ## static lint of both skills (staged as the deployed 'scv' dir)
	cd $(EVAL_DIR) && uv run python run_eval.py --lint-only --tag $(TAG)-lint

eval-run: ## full evaluation; TAG names the reports (reports/<lang>-<TAG>.{md,json})
	cd $(EVAL_DIR) && uv run python run_eval.py --lang $(LANGS) --times $(TIMES) --tag $(TAG)

eval-compare: ## per-case diff with significance bands: $(COMPARE_LANG)-$(OLD_TAG).json vs $(COMPARE_LANG)-$(TAG).json
	cd $(EVAL_DIR) && uv run python run_eval.py --compare reports/$(COMPARE_LANG)-$(OLD_TAG).json reports/$(COMPARE_LANG)-$(TAG).json

eval-clean: ## remove generated reports
	rm -rf $(EVAL_DIR)/reports/*
