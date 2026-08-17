"use strict";

document.addEventListener("htmx:configRequest", function (event) {
  const meta = document.querySelector('meta[name="csrf-token"]');
  if (meta && meta.content) {
    event.detail.headers["X-CSRF-Token"] = meta.content;
  }
});
