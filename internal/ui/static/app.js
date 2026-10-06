// Small progressive enhancements; forms and navigation also work without JS.
document.addEventListener("htmx:afterSwap", (event) => {
  if (event.detail.target.id !== "notes" || event.detail.requestConfig?.verb !== "post") return;
  const input = document.getElementById("title");
  if (event.detail.xhr.status === 200) {
    document.getElementById("app-status").textContent = "Feedbackpunkt gespeichert.";
    input?.focus({ preventScroll: true });
  } else if (event.detail.xhr.status === 422) {
    input?.focus({ preventScroll: true });
  }
});
