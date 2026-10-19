# pipeline (perception fixture)

Complex Go 1.22 module. **SCV perception is Java-only**, so this repo is expected
to report `not-framework` and be skipped — there is no Maven/Gradle marker and
the analyzer ships no Go grammar.

Its purpose is to freeze that behaviour: the structure tier must skip cleanly
(exit 0, status `skipped`) instead of failing.

Covers: generics, interfaces and interface embedding, struct embedding,
goroutines and channels, `sync` primitives, and deliberate mutual recursion
(`Pool.process` <-> `Pool.retry`).
