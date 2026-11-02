// Small progressive enhancements. Every page works without JavaScript.
(function () {
  "use strict";

  // Show a waiting message and prevent double submission on slow actions.
  document.querySelectorAll("form[data-busy], form[data-confirm]").forEach(function (form) {
    form.addEventListener("submit", function (e) {
      var confirmMsg = form.getAttribute("data-confirm");
      if (confirmMsg && !window.confirm(confirmMsg)) {
        e.preventDefault();
        return;
      }
      var busy = form.getAttribute("data-busy");
      var button = form.querySelector("button[type=submit]");
      if (busy && button) {
        button.disabled = true;
        var msg = document.createElement("span");
        msg.className = "busy-msg";
        msg.textContent = busy;
        button.insertAdjacentElement("afterend", msg);
      }
    });
  });

  // Enforce the reference document count before uploading.
  document.querySelectorAll("input[type=file][data-max-files]").forEach(function (input) {
    input.addEventListener("change", function () {
      var max = parseInt(input.getAttribute("data-max-files"), 10);
      if (input.files.length > max) {
        input.setCustomValidity("Choose at most " + max + " reference documents.");
      } else {
        input.setCustomValidity("");
      }
      input.reportValidity();
    });
  });

  // Left/right arrow keys move between questions (unless typing).
  document.addEventListener("keydown", function (e) {
    if (e.altKey || e.ctrlKey || e.metaKey || /INPUT|TEXTAREA|SELECT/.test(document.activeElement.tagName)) return;
    var id = e.key === "ArrowLeft" ? "prev-link" : e.key === "ArrowRight" ? "next-link" : null;
    var link = id && document.getElementById(id);
    if (link) window.location.href = link.href;
  });
})();
