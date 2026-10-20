//! Crate root: module declarations (both `foo.rs` and `bar/mod.rs` file
//! forms) plus the free functions that tie the crate together — a use-path
//! call, a `dyn Trait` receiver, and a `println!` macro (honestly dropped).

pub mod inventory;
pub mod shape;

use crate::inventory::Inventory;
use crate::shape::Shape;

/// One audit cycle: use-path call on a concrete type.
pub fn audit() -> f64 {
    let inv = Inventory { label: "bin-a", units: 3 };
    inv.area()
}

/// Area of any shape through a `dyn Trait` receiver.
pub fn measure(item: &dyn Shape) -> f64 {
    item.area()
}

/// Greets the operator (macro call — dropped and counted, never an edge).
pub fn banner(label: &str) {
    println!("warehouse: {}", label);
}
