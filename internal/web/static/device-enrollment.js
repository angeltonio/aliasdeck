(() => {
  "use strict";

  const checkbox = () => document.getElementById("auto-sync");
  const frequency = () => document.getElementById("sync-frequency");

  function automaticCommand(value) {
    for (const choice of document.querySelectorAll("[data-enrollment-frequency]")) {
      if (choice.dataset.enrollmentFrequency === value) return choice.dataset.command;
    }
    return "";
  }

  function updateEnrollmentCommand() {
    const autoSync = checkbox();
    const select = frequency();
    const field = document.getElementById("sync-frequency-field");
    if (!autoSync || !select || !field) return;

    field.hidden = !autoSync.checked;
    select.disabled = !autoSync.checked;

    const command = document.getElementById("mint-commands");
    if (!command) return;
    command.textContent = autoSync.checked
      ? automaticCommand(select.value) || command.dataset.manualCommand
      : command.dataset.manualCommand;
  }

  document.addEventListener("change", (event) => {
    if (event.target === checkbox() || event.target === frequency()) {
      updateEnrollmentCommand();
    }
  });

  document.addEventListener("click", (event) => {
    const button = event.target.closest("[data-copy-enrollment-command]");
    if (!button) return;
    const command = document.getElementById("mint-commands");
    if (command) navigator.clipboard.writeText(command.innerText);
  });

  document.addEventListener("htmx:afterSwap", updateEnrollmentCommand);
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", updateEnrollmentCommand);
  } else {
    updateEnrollmentCommand();
  }
})();
