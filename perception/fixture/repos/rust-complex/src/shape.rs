//! Geometry trait (with a default method) and a concrete circle.

/// Anything with an area — carries one default method.
pub trait Shape {
    /// The shape's area.
    fn area(&self) -> f64;

    /// Default method: scale the area by a factor.
    fn scale(&self, factor: f64) -> f64 {
        self.area() * factor
    }
}

/// A plain circle.
pub struct Circle {
    pub radius: f64,
}

impl Shape for Circle {
    fn area(&self) -> f64 {
        3.14 * self.radius * self.radius
    }
}
