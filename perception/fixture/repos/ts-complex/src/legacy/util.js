'use strict';

// Legacy helpers kept as CommonJS: a relative require, class this-calls,
// and one unknown free call (the honest unresolved floor for JS).

const { Circle } = require('../models/circle');

class Scanner {
  constructor() {
    this.items = [];
  }

  add(item) {
    this.items[this.items.length] = item;
    return this.describe();
  }

  describe() {
    return this.items.length + ' items';
  }

  first() {
    return this.items[0];
  }
}

function scanCircle(radius) {
  const c = new Circle();
  return c;
}

function broken() {
  return mystery();
}

module.exports = { Scanner, scanCircle };
