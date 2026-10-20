pub struct Cache {
    pub hits: usize,
}

pub enum Mode {
    Fast,
    Slow,
}

pub type Hit = bool;

pub mod policy {
    pub struct Policy {
        pub limit: usize,
    }
}

impl Cache {
    pub fn new() -> Cache {
        Cache { hits: 0 }
    }

    pub fn hit(&self, key: &str) -> String {
        let v = vec![key];
        mystery(key)
    }

    pub fn store(&mut self, key: &str, value: &str) {
        self.hits += 1;
    }
}

impl std::fmt::Display for Cache {
    fn fmt(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result {
        write!(f, "Cache({})", self.hits)
    }
}

pub fn load(_area: f64) -> String {
    String::new()
}
