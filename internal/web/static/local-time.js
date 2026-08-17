(function (global) {
  "use strict";

  function formatLocalTimestamp(datetime, locales, timeZone) {
    var date = new Date(datetime);
    if (Number.isNaN(date.getTime())) {
      return null;
    }

    var parts = new Intl.DateTimeFormat(locales, {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23",
      timeZoneName: "short",
      timeZone: timeZone,
    }).formatToParts(date).reduce(function (values, part) {
      values[part.type] = part.value;
      return values;
    }, {});

    return [
      parts.year + "-" + parts.month + "-" + parts.day,
      parts.hour + ":" + parts.minute,
    ].join(" ");
  }

  function replaceTokens(template, values) {
    return Object.keys(values).reduce(function (text, key) {
      return text.split("{" + key + "}").join(values[key]);
    }, template);
  }

  function renderLocalTimestamps(document) {
    var label = document.getElementById("timestamp-timezone");
    var fallbackZone = label === null ? "" : label.dataset.zoneFallback;
    var timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone || fallbackZone;
    var locales = document.documentElement ? document.documentElement.lang : undefined;
    var timestamps = document.querySelectorAll("time[data-local-time]");

    timestamps.forEach(function (timestamp) {
      var local = formatLocalTimestamp(timestamp.dateTime, locales);
      if (local === null) {
        return;
      }

      timestamp.textContent = local;
	  timestamp.title = replaceTokens(timestamp.dataset.localTitle, { zone: timeZone, utc: timestamp.dataset.utc });
      timestamp.setAttribute("aria-label", timestamp.title);
    });

    if (label !== null) {
      label.textContent = replaceTokens(label.dataset.localLabel, { zone: timeZone });
    }
  }

  var api = {
    formatLocalTimestamp: formatLocalTimestamp,
    renderLocalTimestamps: renderLocalTimestamps,
  };

  if (typeof module !== "undefined" && module.exports) {
    module.exports = api;
  }

  if (typeof document !== "undefined") {
    if (document.readyState === "loading") {
      document.addEventListener("DOMContentLoaded", function () {
        renderLocalTimestamps(document);
      });
    } else {
      renderLocalTimestamps(document);
    }
  }
})(typeof globalThis === "undefined" ? this : globalThis);
