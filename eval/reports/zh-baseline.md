# Skill Evaluation Report: scv

- Path: `/Users/yuhaochen/Documents/codebase/projanvil/SCV/skills/zh-cn`
- Overall: **33.7 / 100**  Grade: **F**
- Parts: behavior 15.6, writing -, lint 78.8

## Behavior Tests

### ❌ run-remote-repo (94 pts · n=2 ±4 · pass^2 ✘)
- ✔ `reference_read(references/run.md)` 1.00 ±0.00 (threshold 1.00) — read 1 time(s)
- ✔ `tool_call(run_command)` 1.00 ±0.00 (threshold 1.00) — run_command called 10 time(s)
- ✔ `no_reference_read(references/gather.md)` 1.00 ±0.00 (threshold 1.00)
- ✔ `no_reference_read(references/batchRun.md)` 1.00 ±0.00 (threshold 1.00)
- ✘ `llm_judge(4 dimensions, threshold=0.7)` 0.69 ±0.19 (threshold 0.70) — weakest: 最终汇报（有清晰的完成报告；subagent 工具不可用时行为得体） (0.0) — The trace ends with 'max_turns reached without a final answer,' so no completion report was produced and the agent kept retrying failed commands (e.g., repeated ls returning 'project1\nproject2') instead of behaving gracefully when tools were unavailable.

### ❌ gather-clone-repo (0 pts · n=2 ±0 · pass^2 ✘)
- Runtime error: all 2 runs failed; first error: APIStatusError: Error code: 402 - {'error': {'message': 'Insufficient Balance (request_id: b4d18126-46a6-4bed-b225-753a2930eb9a)', 'type': 'unknown_error', 'param': None, 'code': 'invalid_request_error'}}
- Run error (counted as 0): APIStatusError: Error code: 402 - {'error': {'message': 'Insufficient Balance (request_id: b4d18126-46a6-4bed-b225-753a2930eb9a)', 'type': 'unknown_error', 'param': None, 'code': 'invalid_request_error'}}
- Run error (counted as 0): APIStatusError: Error code: 402 - {'error': {'message': 'Insufficient Balance (request_id: 5c82456d-b7e9-45cf-bd7f-bc5437d503b5)', 'type': 'unknown_error', 'param': None, 'code': 'invalid_request_error'}}

### ❌ index-rebuild (0 pts · n=2 ±0 · pass^2 ✘)
- Runtime error: all 2 runs failed; first error: APIStatusError: Error code: 402 - {'error': {'message': 'Insufficient Balance (request_id: 700629ec-4c9c-4655-ba9d-a4b36c9f2b30)', 'type': 'unknown_error', 'param': None, 'code': 'invalid_request_error'}}
- Run error (counted as 0): APIStatusError: Error code: 402 - {'error': {'message': 'Insufficient Balance (request_id: 700629ec-4c9c-4655-ba9d-a4b36c9f2b30)', 'type': 'unknown_error', 'param': None, 'code': 'invalid_request_error'}}
- Run error (counted as 0): APIStatusError: Error code: 402 - {'error': {'message': 'Insufficient Balance (request_id: bee25533-cfdd-4817-8d7f-f2a3564618d0)', 'type': 'unknown_error', 'param': None, 'code': 'invalid_request_error'}}

### ❌ batch-analyze-config (0 pts · n=2 ±0 · pass^2 ✘)
- Runtime error: all 2 runs failed; first error: APIStatusError: Error code: 402 - {'error': {'message': 'Insufficient Balance (request_id: 6424720f-d009-4dc3-84cc-9d4a2c76102b)', 'type': 'unknown_error', 'param': None, 'code': 'invalid_request_error'}}
- Run error (counted as 0): APIStatusError: Error code: 402 - {'error': {'message': 'Insufficient Balance (request_id: 6424720f-d009-4dc3-84cc-9d4a2c76102b)', 'type': 'unknown_error', 'param': None, 'code': 'invalid_request_error'}}
- Run error (counted as 0): APIStatusError: Error code: 402 - {'error': {'message': 'Insufficient Balance (request_id: 0aeea27f-281a-43b3-aa5e-d231abb8d03d)', 'type': 'unknown_error', 'param': None, 'code': 'invalid_request_error'}}

### ❌ overview-restraint (0 pts · n=2 ±0 · pass^2 ✘)
- Runtime error: all 2 runs failed; first error: APIStatusError: Error code: 402 - {'error': {'message': 'Insufficient Balance (request_id: 8c5c6820-e165-4f6f-8898-4579a46291c6)', 'type': 'unknown_error', 'param': None, 'code': 'invalid_request_error'}}
- Run error (counted as 0): APIStatusError: Error code: 402 - {'error': {'message': 'Insufficient Balance (request_id: 8c5c6820-e165-4f6f-8898-4579a46291c6)', 'type': 'unknown_error', 'param': None, 'code': 'invalid_request_error'}}
- Run error (counted as 0): APIStatusError: Error code: 402 - {'error': {'message': 'Insufficient Balance (request_id: ffe95ced-c041-41dd-a0b8-76c286df8c90)', 'type': 'unknown_error', 'param': None, 'code': 'invalid_request_error'}}

### ❌ run-local-non-git (0 pts · n=2 ±0 · pass^2 ✘)
- Runtime error: all 2 runs failed; first error: APIStatusError: Error code: 402 - {'error': {'message': 'Insufficient Balance (request_id: 89fe6124-7b64-4527-92dd-8855ecf6bc4a)', 'type': 'unknown_error', 'param': None, 'code': 'invalid_request_error'}}
- Run error (counted as 0): APIStatusError: Error code: 402 - {'error': {'message': 'Insufficient Balance (request_id: 89fe6124-7b64-4527-92dd-8855ecf6bc4a)', 'type': 'unknown_error', 'param': None, 'code': 'invalid_request_error'}}
- Run error (counted as 0): APIStatusError: Error code: 402 - {'error': {'message': 'Insufficient Balance (request_id: 4294bc21-45b7-405f-9adc-1a80ab2433af)', 'type': 'unknown_error', 'param': None, 'code': 'invalid_request_error'}}

## Static Lint

Total 78.8, grade C

| Check | Score | Result |
|---|---|---|
| Metadata (frontmatter / name / description) | 90.0 | ✅ |
| Document structure (sections / substance) | 100.0 | ✅ |
| Trigger coverage (when to use) | 75.0 | ✅ |
| Clarity (placeholders / length) | 100.0 | ✅ |
| Asset references (local file existence) | 60.0 | ✅ |
| Test coverage (cases / examples) | 30.0 | ❌ |

## Writing Review (LLM)

Review failed: APIStatusError: Error code: 402 - {'error': {'message': 'Insufficient Balance (request_id: 788946b8-1f1d-4c0a-bb18-7d90097a73b8)', 'type': 'unknown_error', 'param': None, 'code': 'invalid_request_error'}}

## Reference Coverage

| Reference | Reached | Origin |
|---|---|---|
| references/batchRun.md | unreached | both |
| references/gather.md | unreached | both |
| references/index.md | unreached | both |
| references/project-analyzer.md | unreached | scanned |
| references/run.md | 2× | both |
| references/templates/*.md | unreached | scanned |
| references/templates/ARCHITECTURE.template.md | unreached | declared |
| references/templates/FILE_INDEX.template.md | unreached | declared |
| references/templates/README.template.md | unreached | declared |
| references/templates/SUMMARY.template.md | unreached | declared |

## Challenge (adversarial probes)

> challenger fell back to judge (same model; credibility reduced)

| Type | Trigger | Verdict | Note |
|---|---|---|---|

## Improvement Suggestions

1. [assets] referenced file does not exist: references/project-analyzer.md → create the file or remove the reference
2. [assets] referenced file does not exist: references/templates/*.md → create the file or remove the reference
3. [testcoverage] no test cases / examples / eval files; the skill's actual behavior cannot be verified → add at least one behavior test or eval case
4. [metadata] name (scv) does not match the directory name (zh-cn) → rename the directory or the frontmatter name so they match
5. [trigger] weak trigger guidance (only 1 kind(s) matched: skill self-reference) → add more trigger formulations (keywords, scenarios, both languages if bilingual)

