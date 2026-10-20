const { helper, Tracker } = require("./util");
require("left-pad");

function main() {
  helper(1, 2);
  const t = new Tracker("x");
  mystery();
  parseInt("3");
  console.log("hi");
}

module.exports = { main };
