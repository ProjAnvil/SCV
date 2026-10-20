use store::Registry;

fn main() {
    let r = Registry::new();
    r.put("k", "v");
}
