import { test } from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import path from "node:path";

const require = createRequire(import.meta.url);
const here = path.dirname(fileURLToPath(import.meta.url));
const keys = require(path.join(here, "..", "..", "internal", "web", "static", "keys.js"));

const ev = (o) => ({ type: "keydown", key: "Enter", shiftKey: true, ctrlKey: false, altKey: false, metaKey: false, ...o });

test("Shift+Enter keydown matches", () => {
  assert.equal(keys.shiftEnterNewline(ev({})), true);
});

test("everything else does not", () => {
  assert.equal(keys.shiftEnterNewline(ev({ shiftKey: false })), false);
  assert.equal(keys.shiftEnterNewline(ev({ ctrlKey: true })), false);
  assert.equal(keys.shiftEnterNewline(ev({ altKey: true })), false);
  assert.equal(keys.shiftEnterNewline(ev({ metaKey: true })), false);
  assert.equal(keys.shiftEnterNewline(ev({ key: "a" })), false);
  assert.equal(keys.shiftEnterNewline(ev({ type: "keyup" })), false);
  assert.equal(keys.shiftEnterNewline(ev({ type: "keypress" })), false);
});
