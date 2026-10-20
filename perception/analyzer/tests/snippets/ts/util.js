function helper(a, b) {
  return a + b;
}

class Tracker {
  constructor(name) {
    this.name = name;
  }
  track(item) {
    this.flush(item);
  }
  flush(item) {
    return item;
  }
}

module.exports = { helper, Tracker };
