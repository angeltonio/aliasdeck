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

  function localTimeZone() {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "your local time zone";
  }

  function renderLocalTimestamps(document) {
    var timeZone = localTimeZone();
    var timestamps = document.querySelectorAll("time[data-local-time]");

    timestamps.forEach(function (timestamp) {
      var local = formatLocalTimestamp(timestamp.dateTime);
      if (local === null) {
        return;
      }

      timestamp.textContent = local;
      timestamp.title = "Local time (" + timeZone + "). UTC: " + timestamp.dataset.utc;
      timestamp.setAttribute("aria-label", timestamp.title);
    });

    var label = document.getElementById("timestamp-timezone");
    if (label !== null) {
      label.textContent = "Timestamps are shown in your local browser time (" + timeZone + ").";
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
