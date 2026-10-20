//! Warehouse inventory: a second Shape impl plus an external-trait impl
//! (`std::fmt::Display`) that is honestly dropped — one end of the
//! relation is not an in-source node.

use std::fmt;

use crate::shape::Shape;

/// Everything stored in one bin.
pub struct Inventory {
    pub label: &'static str,
    pub units: u32,
}

impl Shape for Inventory {
    fn area(&self) -> f64 {
        self.units as f64
    }
}

impl fmt::Display for Inventory {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}:{}", self.label, self.units)
    }
}
