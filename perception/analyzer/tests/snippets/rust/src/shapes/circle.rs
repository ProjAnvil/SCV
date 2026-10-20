use super::Shape;

pub struct Circle {
    pub radius: f64,
}

impl Circle {
    pub fn new(radius: f64) -> Circle {
        Circle { radius }
    }
}

impl Shape for Circle {
    fn area(&self) -> f64 {
        3.14 * self.radius
    }
}
