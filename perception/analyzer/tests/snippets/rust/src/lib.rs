pub mod shapes;
pub mod cache;

use crate::shapes::circle::Circle;

pub trait Store {
    fn get(&self, key: &str) -> String;
    fn put(&mut self, key: &str, value: &str);
}

pub struct Registry {
    pub cache: cache::Cache,
}

impl Registry {
    pub fn new() -> Registry {
        Registry { cache: cache::Cache::new() }
    }

    pub fn refresh(&mut self, key: &str) -> String {
        self.cache.hit(key)
    }
}

impl Store for Registry {
    fn get(&self, key: &str) -> String {
        self.refresh(key)
    }

    fn put(&mut self, key: &str, value: &str) {
        self.cache.store(key, value);
    }
}

pub fn lookup(store: &dyn Store, key: &str) -> String {
    store.get(key)
}

pub fn describe() -> String {
    let c = Circle::new(2.0);
    crate::cache::load(c.area())
}
