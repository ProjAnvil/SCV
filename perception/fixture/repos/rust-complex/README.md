# rust-complex (perception fixture)

A small Rust crate used as a positive ground truth for SCV perception v3.
It is not meant to compile against a real cargo registry; it exists so
the analyzer sees every rust edge kind in one crate.

Covers on purpose:

- `Cargo.toml` crate root (`src/lib.rs`) with both module file forms:
  `pub mod shape;` -> `shape.rs`, `pub mod inventory;` ->
  `inventory/mod.rs`
- a trait with a default method (`Shape::scale` calls `self.area()` —
  `virtual-dispatch`)
- two `impl Trait for Type` blocks (Circle, Inventory) — IMPLEMENTS is
  fully explicit (`kind: declared`)
- a `dyn Trait` receiver (`measure(item: &dyn Shape)`) —
  `virtual-dispatch`
- use-path resolution: `use crate::shape::Shape` powers the CALLS and
  File->Type REFERENCES edges
- an external-trait impl (`impl fmt::Display for Inventory`) — the trait
  side is not in-source, so the edge is dropped and counted
- a macro call (`println!` in `banner`) — dropped and counted

## Frozen ground truth (hand-derived, spec §11.2)

Hand-counted against the extractor rules (rust_extractor: crate root
`src/lib.rs`, `mod` tree walk, `use` binding in stage 3, impl binding in
stage 4):

- nodes: 3 File, 3 Type (Shape trait, Circle struct, Inventory struct),
  7 Method (Shape#area, Shape#scale, Circle#area, Inventory#area,
  audit, measure, banner)
- CONTAINS 3; IMPLEMENTS 2 (Circle->Shape, Inventory->Shape, both
  `kind: declared`); no EXTENDS (Rust has no base classes)
- CALLS 3: Shape#scale->Shape#area (virtual-dispatch),
  audit->Inventory#area (direct), measure->Shape#area (virtual-dispatch)
- REFERENCES 5: lib.rs->Inventory and lib.rs->Shape (use bindings),
  inventory/mod.rs->Shape (use binding), audit->Inventory (struct
  literal), measure->Shape (signature type)
- IMPORTS 3: lib.rs -> inventory/mod.rs, lib.rs -> shape.rs,
  inventory/mod.rs -> shape.rs
- `dropped_edges` 2: the `impl fmt::Display` external-trait edge + the
  `println!` macro site
- `unresolved_refs` 0; `meta.languages == ["rust"]`
