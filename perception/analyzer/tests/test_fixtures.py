"""Frozen-fixture ground-truth suite — the §5 multiset bar (Task 4).

Spec 2026-10-14 §5 "Precision contract": per fixture, the tree-sitter
analyzer (run with ``--extensions springboot``) must reproduce the v1
Spoon ground truth EXACTLY as a semantic multiset — bean nodes matched on
(key, stereotype, conditions), INJECTS edges on (srcKey, dstKey, via,
qualifier, wraps, ambiguous). Order and positional props (at/source) are
out of scope; both engines' dumps are projected through the SAME tuple
projection before comparison.

Expected literals below were DERIVED REPRODUCIBLY from the in-tree Spoon
engine (never hand-typed), per the plan's Task-4 procedure:

    cd perception/adapters/springboot && gradle installDist -q
    ./build/install/springboot-probe/bin/springboot-probe gradle <fixture-copy> /tmp/v1-<name>.json
    python3 -c "import json,sys; d=json.load(open(sys.argv[1])); \\
        print(sorted((n['key'],n['props'].get('stereotype'),n['props'].get('conditions')) for n in d['nodes'])); \\
        print(sorted((e['srcKey'],e['dstKey'],e['props'].get('via'),e['props'].get('qualifier'), \\
        e['props'].get('wraps'),e['props'].get('ambiguous',False)) for e in d['edges']))" /tmp/v1-<name>.json

Derivation mode: GRADLE for all 5 fixtures. The plan's first-pass
``noclasspath`` mode is measurably DEGRADED on every fixture (it loses
classpath resolution: gradle-lombok-sample 11/9 vs 12/8, boot2-legacy
14/10 vs 15/10, boot3-modern 11/6 vs 12/6, boot2-multi 5/4 vs 6/4,
torture-app 9/5 with unresolved injections vs 30/24/0), so the multiset
bar uses the v1 GRADLE-mode ground truth, which the frozen regression
suite (``gradle test``) asserts. The v1 dumps are Bean/INJECTS-only, so
the canonical all-nodes projection equals the bean-layer projection; the
tree-sitter side filters label == "Bean"/"INJECTS" (its output also
carries the structure layer).

Fixtures are FROZEN: each one is copied to a temp dir before analysis so
the source tree is never touched (Spoon especially would build inside
it). Also in this module: the tiny non-Spring plain-java fixture
(analyzer-level assertions only — the perceive-level DETECT assertion
belongs to T5/T7).
"""

import json
import shutil
from pathlib import Path

import pytest

from analyzer import main as analyzer_main

TESTS_DIR = Path(__file__).resolve().parent
SCV_ROOT = TESTS_DIR.parents[2]
FIXTURES_ROOT = (SCV_ROOT / "perception" / "adapters" / "springboot" / "src" /
                 "test" / "resources" / "fixtures")
PLAIN_JAVA = TESTS_DIR / "fixtures" / "plain-java"

SPRINGBOOT_FIXTURES = (
    "gradle-lombok-sample",
    "boot2-legacy",
    "boot3-modern",
    "boot2-multi",
    "torture-app",
)


# ---- Spoon-derived ground truth (GRADLE mode, see module docstring) ----

# gradle-lombok-sample: 12 beans / 8 INJECTS edges
GRADLE_LOMBOK_SAMPLE_BEANS = [
    ('bean:appConfig', 'Configuration', None),
    ('bean:auditService', 'Component', 'ConditionalOnProperty(audit.enabled=true)'),
    ('bean:demoApplication', 'Configuration', None),
    ('bean:emailSender', 'Component', None),
    ('bean:greetingClient', '@Bean', None),
    ('bean:greetingController', 'RestController', None),
    ('bean:greetingServiceImpl', 'Service', None),
    ('bean:inMemoryGreetingRepository', 'Repository', None),
    ('bean:legacyCacheService', 'Component', 'Profile(legacy)'),
    ('bean:reportService', 'Service', None),
    ('bean:smsSender', 'Component', None),
    ('bean:systemClock', '@Bean', None),
]
GRADLE_LOMBOK_SAMPLE_EDGES = [
    ('bean:greetingClient', 'bean:greetingServiceImpl', 'beanMethod', None, None, False),
    ('bean:greetingClient', 'bean:systemClock', 'beanMethod', None, None, False),
    ('bean:greetingController', 'bean:greetingServiceImpl', 'lombok-constructor', None, None, False),
    ('bean:greetingServiceImpl', 'bean:emailSender', 'lombok-constructor', 'emailSender', None, False),
    ('bean:greetingServiceImpl', 'bean:inMemoryGreetingRepository', 'lombok-constructor', None, None, False),
    ('bean:reportService', 'bean:emailSender', 'field', None, None, True),
    ('bean:reportService', 'bean:inMemoryGreetingRepository', 'setter', None, None, False),
    ('bean:reportService', 'bean:smsSender', 'field', None, None, True),
]

# boot2-legacy: 15 beans / 10 INJECTS edges (4 ambiguous)
BOOT2_LEGACY_BEANS = [
    ('bean:appConfig', 'Configuration', None),
    ('bean:clock', '@Bean', None),
    ('bean:devOnlyBean', 'Component', 'Profile(dev)'),
    ('bean:emailChannel', 'Component', None),
    ('bean:fancyRepository', 'Repository', None),
    ('bean:globalAdvice', 'ControllerAdvice', None),
    ('bean:legacyApplication', 'Configuration', None),
    ('bean:mockGreetingService', '@Bean', 'Profile(dev)'),
    ('bean:notifierService', 'Service', None),
    ('bean:orderController', 'Controller', None),
    ('bean:prototypeBean', 'Component', None),
    ('bean:realGreetingService', '@Bean', 'Profile(prod)'),
    ('bean:reportService', 'Service', None),
    ('bean:smsChannel', 'Component', None),
    ('bean:webConfig', 'Configuration', None),
]
BOOT2_LEGACY_EDGES = [
    ('bean:globalAdvice', 'bean:fancyRepository', 'constructor', None, None, False),
    ('bean:notifierService', 'bean:clock', 'constructor', None, None, False),
    ('bean:notifierService', 'bean:fancyRepository', 'constructor', None, None, False),
    ('bean:orderController', 'bean:emailChannel', 'field', None, None, True),
    ('bean:orderController', 'bean:fancyRepository', 'inherited-field', None, None, False),
    ('bean:orderController', 'bean:smsChannel', 'field', None, None, True),
    ('bean:realGreetingService', 'bean:fancyRepository', 'beanMethod', None, None, False),
    ('bean:reportService', 'bean:fancyRepository', 'constructor', None, None, False),
    ('bean:reportService', 'bean:mockGreetingService', 'field', None, None, True),
    ('bean:reportService', 'bean:realGreetingService', 'field', None, None, True),
]

# boot3-modern: 12 beans / 6 INJECTS edges
BOOT3_MODERN_BEANS = [
    ('bean:appConfig', 'Configuration', None),
    ('bean:asyncConfig', 'Configuration', None),
    ('bean:auditSink', 'Component', None),
    ('bean:cardChannel', 'Component', None),
    ('bean:checkoutService', 'Service', None),
    ('bean:eventListenerService', 'Component', None),
    ('bean:ledgerService', 'Component', None),
    ('bean:modernApplication', 'Configuration', None),
    ('bean:orderMapper', 'Repository', None),
    ('bean:paymentProps', 'ConfigurationProperties', 'EnableConfigurationProperties(AppConfig)'),
    ('bean:switchExprRouter', 'Component', None),
    ('bean:walletChannel', 'Component', None),
]
BOOT3_MODERN_EDGES = [
    ('bean:cardChannel', 'bean:paymentProps', 'constructor', None, None, False),
    ('bean:checkoutService', 'bean:auditSink', 'lombok-constructor', None, None, False),
    ('bean:checkoutService', 'bean:cardChannel', 'lombok-constructor', None, None, False),
    ('bean:eventListenerService', 'bean:auditSink', 'constructor', None, None, False),
    ('bean:ledgerService', 'bean:orderMapper', 'setter', None, None, False),
    ('bean:switchExprRouter', 'bean:checkoutService', 'constructor', None, None, False),
]

# boot2-multi: 6 beans / 4 INJECTS edges
BOOT2_MULTI_BEANS = [
    ('bean:boot2MultiApplication', 'Configuration', None),
    ('bean:customerController', 'Controller', None),
    ('bean:customerRepository', 'Repository', None),
    ('bean:customerService', 'Service', None),
    ('bean:loyaltyController', 'Controller', None),
    ('bean:loyaltyService', 'Service', None),
]
BOOT2_MULTI_EDGES = [
    ('bean:customerController', 'bean:customerService', 'constructor', None, None, False),
    ('bean:customerService', 'bean:customerRepository', 'constructor', None, None, False),
    ('bean:loyaltyController', 'bean:loyaltyService', 'constructor', None, None, False),
    ('bean:loyaltyService', 'bean:customerRepository', 'field', None, None, False),
]

# torture-app: 30 beans / 24 INJECTS edges (4 ambiguous, incl. the deliberate
# circularA <-> circularB pair); note paymentService -> cardPaymentHandler
# appears TWICE with different ambiguous flags — multiset, not set.
TORTURE_APP_BEANS = [
    ('bean:URLValidator', 'Component', None),
    ('bean:appConfig', 'Configuration', None),
    ('bean:broadcastService', 'Service', None),
    ('bean:cacheWarmer', 'Component', None),
    ('bean:cardPaymentHandler', 'Component', None),
    ('bean:cashPaymentHandler', 'Component', None),
    ('bean:circularA', 'Component', None),
    ('bean:circularB', 'Component', None),
    ('bean:clock', '@Bean', None),
    ('bean:customPolicy', '@Bean', 'ConditionalOnProperty(sched.custom.enabled=true)'),
    ('bean:defaultPolicy', '@Bean', 'ConditionalOnMissingBean'),
    ('bean:emailGreetingSender', 'Component', None),
    ('bean:executorInspector', 'Component', None),
    ('bean:featureProperties', 'ConfigurationProperties', 'EnableConfigurationProperties(AppConfig)'),
    ('bean:globalExceptionHandler', 'ControllerAdvice', None),
    ('bean:greetingAudit', 'Component', None),
    ('bean:greetingClient', '@Bean', None),
    ('bean:greetingController', 'RestController', None),
    ('bean:greetingProperties', 'Component', None),
    ('bean:greetingServiceImpl', 'Service', None),
    ('bean:jpaGreetingRepository', 'Repository', None),
    ('bean:legacyCompatService', 'Service', None),
    ('bean:metrics', 'Component', None),
    ('bean:multiCtorService', 'Service', None),
    ('bean:optionalGreetingService', 'Service', None),
    ('bean:paymentService', 'Service', None),
    ('bean:smsGreetingSender', 'Component', None),
    ('bean:templateEngine', 'Component', None),
    ('bean:tortureApplication', 'Configuration', None),
    ('bean:urgentSenderService', 'Service', None),
]
TORTURE_APP_EDGES = [
    ('bean:broadcastService', 'bean:emailGreetingSender', 'lombok-constructor', None, 'List', False),
    ('bean:broadcastService', 'bean:greetingAudit', 'lombok-constructor', None, None, False),
    ('bean:broadcastService', 'bean:smsGreetingSender', 'lombok-constructor', None, 'List', False),
    ('bean:circularA', 'bean:circularB', 'field', None, None, False),
    ('bean:circularB', 'bean:circularA', 'field', None, None, False),
    ('bean:executorInspector', 'bean:customPolicy', 'constructor', None, None, True),
    ('bean:executorInspector', 'bean:defaultPolicy', 'constructor', None, None, True),
    ('bean:globalExceptionHandler', 'bean:greetingAudit', 'constructor', None, None, False),
    ('bean:greetingClient', 'bean:clock', 'beanMethod', None, None, False),
    ('bean:greetingClient', 'bean:emailGreetingSender', 'beanMethod', 'emailGreetingSender', None, False),
    ('bean:greetingController', 'bean:emailGreetingSender', 'constructor', 'emailGreetingSender', None, False),
    ('bean:greetingController', 'bean:greetingServiceImpl', 'constructor', None, None, False),
    ('bean:greetingServiceImpl', 'bean:greetingAudit', 'inherited-field', None, None, False),
    ('bean:greetingServiceImpl', 'bean:jpaGreetingRepository', 'constructor', None, None, False),
    ('bean:jpaGreetingRepository', 'bean:greetingAudit', 'constructor', None, None, False),
    ('bean:legacyCompatService', 'bean:greetingAudit', 'constructor', None, None, False),
    ('bean:metrics', 'bean:greetingAudit', 'constructor', None, None, False),
    ('bean:multiCtorService', 'bean:greetingAudit', 'constructor', None, None, False),
    ('bean:optionalGreetingService', 'bean:cacheWarmer', 'constructor', None, 'Optional', False),
    ('bean:paymentService', 'bean:cardPaymentHandler', 'field', None, None, False),
    ('bean:paymentService', 'bean:cardPaymentHandler', 'field', None, None, True),
    ('bean:paymentService', 'bean:cashPaymentHandler', 'field', None, None, True),
    ('bean:templateEngine', 'bean:greetingProperties', 'constructor', None, None, False),
    ('bean:urgentSenderService', 'bean:smsGreetingSender', 'constructor', None, None, False),
]

GROUND_TRUTH = {
    "gradle-lombok-sample": (GRADLE_LOMBOK_SAMPLE_BEANS, GRADLE_LOMBOK_SAMPLE_EDGES),
    "boot2-legacy": (BOOT2_LEGACY_BEANS, BOOT2_LEGACY_EDGES),
    "boot3-modern": (BOOT3_MODERN_BEANS, BOOT3_MODERN_EDGES),
    "boot2-multi": (BOOT2_MULTI_BEANS, BOOT2_MULTI_EDGES),
    "torture-app": (TORTURE_APP_BEANS, TORTURE_APP_EDGES),
}


# ---- helpers ----

def bean_tuples(payload: dict) -> list[tuple]:
    """§5 projection: bean nodes as (key, stereotype, conditions)."""
    return sorted(
        (n["key"], n["props"].get("stereotype"), n["props"].get("conditions"))
        for n in payload["nodes"] if n["label"] == "Bean")


def injects_tuples(payload: dict) -> list[tuple]:
    """§5 projection: INJECTS edges as (src, dst, via, qualifier, wraps, ambiguous)."""
    return sorted(
        (e["srcKey"], e["dstKey"], e["props"].get("via"), e["props"].get("qualifier"),
         e["props"].get("wraps"), e["props"].get("ambiguous", False))
        for e in payload["edges"] if e["label"] == "INJECTS")


@pytest.fixture(scope="module")
def payloads(tmp_path_factory):
    """Analyze each frozen fixture ONCE (temp copy + ``--extensions
    springboot`` via the CLI entry) and cache the parsed payload."""
    cache: dict[str, dict] = {}

    def get(name: str) -> dict:
        if name not in cache:
            repo = tmp_path_factory.mktemp(name) / name
            shutil.copytree(FIXTURES_ROOT / name, repo)
            out_json = repo.parent / f"{name}.json"
            assert analyzer_main(
                [str(repo), str(out_json), "--extensions", "springboot"]) == 0
            cache[name] = json.loads(out_json.read_text(encoding="utf-8"))
        return cache[name]

    return get


# ---- the multiset bar (spec §5 precision contract, §8.2) ----

@pytest.mark.parametrize("name", SPRINGBOOT_FIXTURES)
def test_fixture_bean_multiset_matches_spoon_ground_truth(payloads, name):
    beans, _edges = GROUND_TRUTH[name]
    payload = payloads(name)
    assert bean_tuples(payload) == sorted(beans)
    meta = payload["meta"]
    assert meta["beans"] == len(beans)


@pytest.mark.parametrize("name", SPRINGBOOT_FIXTURES)
def test_fixture_injects_multiset_matches_spoon_ground_truth(payloads, name):
    _beans, edges = GROUND_TRUTH[name]
    payload = payloads(name)
    assert injects_tuples(payload) == sorted(edges)
    meta = payload["meta"]
    assert meta["inject_edges"] == len(edges)
    assert meta["ambiguous"] == sum(1 for e in edges if e[5])


@pytest.mark.parametrize("name", SPRINGBOOT_FIXTURES)
def test_fixture_no_unresolved_injections(payloads, name):
    """v1 regression bar: 0 unresolved injections on every fixture."""
    assert payloads(name)["unresolved"] == 0


# ---- torture-app structure meta bounds (plan Task 4, spec §8.3) ----

def test_torture_app_structure_meta_bounds(payloads):
    st = payloads("torture-app")["meta"]["structure"]
    assert st["files"] == 33
    assert st["methods"] == 52  # pinned to the true syntactic count (T1 note)
    assert 10 <= st["call_edges"] <= 40  # in-source resolution only
    assert isinstance(st["key_collisions"], int)  # reported, never silent
    assert st["dropped_edges"] > 0  # CALLS drops counted honestly


# ---- plain-java fixture (analyzer level only; DETECT belongs to T5/T7) ----

def test_plain_java_fixture_layout_has_no_spring_markers():
    """Fixture guard: a pom.xml build marker with zero spring content."""
    pom = (PLAIN_JAVA / "pom.xml").read_text(encoding="utf-8")
    assert "<project" in pom
    assert "spring" not in pom.lower()
    java_files = sorted(PLAIN_JAVA.rglob("*.java"))
    assert len(java_files) == 3


def test_plain_java_structure_mode_no_beans(tmp_path):
    """Non-Spring java tree: structure counts present, no Bean label, no
    INJECTS edges — even with the springboot extension enabled."""
    repo = tmp_path / "plain-java"
    shutil.copytree(PLAIN_JAVA, repo)
    out_json = tmp_path / "structure.json"
    assert analyzer_main([str(repo), str(out_json), "--extensions", "springboot"]) == 0
    payload = json.loads(out_json.read_text(encoding="utf-8"))

    labels = {n["label"] for n in payload["nodes"]}
    assert "Bean" not in labels
    edge_labels = {e["label"] for e in payload["edges"]}
    assert "INJECTS" not in edge_labels

    st = payload["meta"]["structure"]
    assert st["files"] == 3
    assert st["types"] == 3
    assert st["methods"] == 4
    assert st["call_edges"] == 2
    meta = payload["meta"]
    assert meta["beans"] == 0
    assert meta["inject_edges"] == 0
    assert meta["extensions"] == ["springboot"]
