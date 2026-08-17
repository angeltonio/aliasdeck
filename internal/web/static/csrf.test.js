"use strict";

const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");

function loadHandler(token) {
  let handler;
  const document = {
    addEventListener(name, callback) {
      assert.equal(name, "htmx:configRequest");
      handler = callback;
    },
    querySelector(selector) {
      assert.equal(selector, 'meta[name="csrf-token"]');
      return token === null ? null : { content: token };
    },
  };
  const source = fs.readFileSync(path.join(__dirname, "csrf.js"), "utf8");
  vm.runInNewContext(source, { document });
  return handler;
}

test("adds the session CSRF token to every HTMX request", () => {
  const headers = {};
  loadHandler("session-token")({ detail: { headers } });
  assert.equal(headers["X-CSRF-Token"], "session-token");
});

test("does not add an empty or missing token", () => {
  for (const token of ["", null]) {
    const headers = {};
    loadHandler(token)({ detail: { headers } });
    assert.deepEqual(headers, {});
  }
});
