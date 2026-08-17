const assert = require("node:assert/strict");
const test = require("node:test");

const { formatLocalTimestamp, renderLocalTimestamps } = require("./local-time.js");

test("formats an RFC 3339 UTC timestamp in the browser time zone", () => {
  assert.equal(
    formatLocalTimestamp("2030-01-01T00:00:00Z", "en-GB", "Europe/Madrid"),
    "2030-01-01 01:00",
  );
});

test("leaves an invalid datetime untouched", () => {
  assert.equal(formatLocalTimestamp("not-a-timestamp", "en-GB", "UTC"), null);
});

test("renders local text and updates the visible time zone label", () => {
  const attributes = new Map();
  const timestamp = {
    dateTime: "2030-01-01T00:00:00Z",
    dataset: {
      utc: "2030-01-01 00:00 UTC",
      localTitle: "Hora local ({zone}). UTC: {utc}",
    },
    textContent: "2030-01-01 00:00 UTC",
    title: "UTC timestamp.",
    setAttribute(name, value) {
      attributes.set(name, value);
    },
  };
  const label = {
    dataset: {
      localLabel: "Las marcas de tiempo se muestran en la hora local del navegador ({zone}).",
      zoneFallback: "zona horaria local",
    },
    textContent: "Las marcas de tiempo se muestran en UTC.",
  };
  const document = {
    documentElement: { lang: "es" },
    querySelectorAll(selector) {
      assert.equal(selector, "time[data-local-time]");
      return [timestamp];
    },
    getElementById(id) {
      assert.equal(id, "timestamp-timezone");
      return label;
    },
  };

  renderLocalTimestamps(document);

  assert.notEqual(timestamp.textContent, "2030-01-01 00:00 UTC");
  assert.match(timestamp.title, /^Hora local \(.+\)\. UTC: 2030-01-01 00:00 UTC$/);
  assert.equal(attributes.get("aria-label"), timestamp.title);
  assert.match(label.textContent, /^Las marcas de tiempo se muestran en la hora local del navegador \(.+\)\.$/);
});
