# analytics-fixture (perception fixture)

Complex Python 3.11 project. **SCV perception is Java-only**, so this repo is
expected to report `not-framework` and be skipped: there is no `pom.xml` or
`build.gradle` marker, and the analyzer has no Python grammar.

Its purpose is to freeze that behaviour — the structure tier must skip cleanly
(exit 0, status `skipped`) rather than fail or emit an empty structure graph.

Covers: dataclasses, enums, generics, ABCs, metaclasses, mixins, decorators,
async iteration, context managers, and deliberate mutual recursion
(`Pipeline._dedupe` <-> `Pipeline._flatten`).
