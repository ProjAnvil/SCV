# pipeline (perception fixture)

Complex Go 1.22 module, used as a positive ground truth for SCV
perception v3's Go extractor — including the §5.1 syntactic method-set
IMPLEMENTS relation (the one relation graphify punts on).

Covers on purpose:

- generics, interfaces and interface embedding (Store = Reader + Writer)
- struct embedding: `ReadOnlyLog` embeds `*Memory` and earns its
  Reader/Writer/Store coverage through **promotion** (IMPLEMENTS with
  `receiver: "value"`)
- pointer-receiver-only coverage: `Memory` (and `AuditLog`) satisfy the
  interfaces only through pointer-receiver methods (IMPLEMENTS with
  `receiver: "pointer"`)
- the **honest-miss case**: `AuditLog` embeds an external `*sync.Mutex`;
  whether it satisfies the in-source `Locker` interface depends on
  promoted methods that no syntactic pass can know — no edge, counted in
  `meta.structure.go_unverifiable_satisfaction` (frozen at 1)
- goroutines and channels, `sync` primitives, and deliberate mutual
  recursion (`Pool.process` <-> `Pool.retry`)

Frozen IMPLEMENTS multiset (§5.1.5 precision contract), 9 edges:

- `Memory -> Reader / Writer / Store` (`method-set`, `receiver: pointer`)
- `AuditLog -> Reader / Writer / Store` (`method-set`, `receiver: pointer`)
- `ReadOnlyLog -> Reader / Writer / Store` (`method-set`, `receiver: value`,
  promotion-earned)

plus `go_unverifiable_satisfaction == 1` (AuditLog vs Locker) and the
external-embed EXTENDS drop (`*sync.Mutex`, counted in `dropped_edges`).
