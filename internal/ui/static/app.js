// Browser behaviour only. Records, permissions and workflow decisions stay on the server.
(() => {
  try { localStorage.removeItem("htmx-history-cache"); sessionStorage.removeItem("htmx-history-cache"); } catch (_) {}
  const requests = new Map();
  const announce = (text) => { const node = document.getElementById("app-status"); if (node) node.textContent = text; };
  const busy = () => {
    const pending = requests.size > 0;
    const frame = document.getElementById("app-frame");
    frame?.setAttribute("aria-busy", String(pending));
    if (frame) frame.inert = Array.from(requests.values()).some((record) => !record.search);
    document.getElementById("page-loading")?.classList.toggle("is-loading", pending);
  };
  const cleanup = (xhr) => {
    const record = requests.get(xhr);
    if (record) {
      for (const button of record.buttons) {
        if (button.isConnected) {
          button.textContent = button.dataset.previousLabel || button.textContent;
          delete button.dataset.previousLabel;
          button.disabled = button.dataset.previousDisabled === "true";
          delete button.dataset.previousDisabled;
        }
      }
      requests.delete(xhr);
    }
    busy();
  };
  const showNetwork = (event) => {
    const notice = document.getElementById("network-error");
    if (!notice) return;
    const verb = event.detail?.requestConfig?.verb || requests.get(event.detail?.xhr)?.verb || "get";
    notice.querySelector("p").textContent = verb === "get" ? "Die Seite konnte nicht geladen werden. Bitte prüfe deine Verbindung und versuche es erneut." : "Die Verbindung wurde unterbrochen. Die Aktion könnte bereits gespeichert sein. Lade den aktuellen Stand, bevor du sie erneut ausführst.";
    notice.hidden = false;
    cleanup(event.detail?.xhr);
  };
  document.addEventListener("htmx:beforeRequest", (event) => {
    const { xhr, elt, requestConfig } = event.detail;
    const form = elt?.closest("form");
    const active = document.activeElement;
    const queueRow = elt?.closest("[data-request-id]");
    const queueRows = queueRow ? Array.from(queueRow.parentElement.querySelectorAll("[data-request-id]")) : [];
    const queueIndex = queueRows.indexOf(queueRow);
    const nextQueueIDs = queueIndex < 0 ? [] : queueRows.slice(queueIndex + 1).concat(queueRows.slice(0, queueIndex).reverse()).map((row) => row.dataset.requestId);
    const buttons = requestConfig?.verb === "get" ? [] : Array.from(form?.querySelectorAll("button[type=submit]") || []);
    for (const button of buttons) {
      button.dataset.previousLabel = button.textContent;
      button.dataset.previousDisabled = String(button.disabled);
      button.textContent = button.dataset.pendingLabel || "Wird gespeichert …";
      button.disabled = true;
    }
    requests.set(xhr, {
      buttons,
      search: requestConfig?.verb === "get" && form?.contains(active) && active?.matches("input[type=search]"),
      verb: requestConfig?.verb || "get",
      activeID: active?.id,
      selection: active?.selectionStart,
      selectionEnd: active?.selectionEnd,
      selectionDirection: active?.selectionDirection,
      scrollX: window.scrollX,
      scrollY: window.scrollY,
      nextQueueIDs,
    });
    document.getElementById("network-error")?.setAttribute("hidden", "");
    busy(); announce(requestConfig?.verb === "get" ? "Seite wird geladen." : "Aktion wird gespeichert.");
  });
  document.addEventListener("htmx:beforeSwap", (event) => {
    const detail = event.detail;
    if (detail.xhr.status === 0) return;
    if (!detail.xhr.responseText.includes('id="app-frame"')) {
      detail.shouldSwap = false;
      showNetwork(event);
      return;
    }
    detail.isError = false;
  });
  document.addEventListener("htmx:afterSwap", (event) => {
    if (event.detail.target.id !== "app-frame") return;
    const record = requests.get(event.detail.xhr);
    const error = document.getElementById("form-errors");
    const previous = record?.activeID && document.getElementById(record.activeID);
    if (error) {
      error.focus({ preventScroll: true });
      error.scrollIntoView({ block: "start", behavior: "instant" });
    } else if (record?.search && previous?.matches("input[type=search]")) {
      previous.focus({ preventScroll: true });
      try { previous.setSelectionRange(record.selection, record.selectionEnd, record.selectionDirection); } catch (_) {}
      window.scrollTo({ left: record.scrollX, top: record.scrollY, behavior: "instant" });
    } else {
      // A route or pagination transition starts at its heading. Queue decisions
      // can keep the next remaining action in view instead of jumping away.
      const queueRows = Array.from(document.querySelectorAll("[data-request-id]"));
      const nextRow = record?.verb !== "get" ? record?.nextQueueIDs?.map((id) => queueRows.find((row) => row.dataset.requestId === id)).find(Boolean) : null;
      const nextAction = nextRow?.querySelector("button[type=submit]");
      if (nextAction) {
        window.scrollTo({ left: record.scrollX, top: record.scrollY, behavior: "instant" });
        nextAction.focus({ preventScroll: true });
        nextAction.scrollIntoView({ block: "nearest", behavior: "instant" });
      } else {
        document.getElementById("main")?.focus({ preventScroll: true });
        window.scrollTo({ left: 0, top: 0, behavior: "instant" });
      }
    }
    announce(error ? "Bitte prüfe deine Angaben." : "Seite aktualisiert.");
  });
  for (const name of ["htmx:afterRequest", "htmx:sendAbort", "htmx:abort", "htmx:responseError"]) {
    document.addEventListener(name, (event) => cleanup(event.detail?.xhr));
  }
  for (const name of ["htmx:sendError", "htmx:timeout", "htmx:swapError", "htmx:targetError"]) document.addEventListener(name, showNetwork);
  document.addEventListener("change", (event) => {
    if (!event.target.matches("input[name=shift][type=checkbox]")) return;
    const output = document.getElementById("signup-selected-count");
    if (output) output.textContent = event.target.closest("form").querySelectorAll("input[name=shift]:checked").length;
  });
  document.addEventListener("click", async (event) => {
    const copy = event.target.closest("[data-copy-target]");
    if (copy) {
      const input = document.getElementById(copy.dataset.copyTarget);
      if (!input) return;
      try { await navigator.clipboard.writeText(input.value); announce("Link kopiert."); copy.textContent = "Link kopiert"; }
      catch (_) { input.focus(); input.select(); announce("Bitte markierten Link manuell kopieren."); }
    }
    if (event.target.closest("[data-refresh]")) location.reload();
    if (event.target.closest("[data-print]")) window.print();
    const menu = document.getElementById("workspace-menu");
    if (menu?.open && !menu.contains(event.target)) menu.open = false;
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return;
    const menu = document.getElementById("workspace-menu");
    if (menu?.open) { menu.open = false; menu.querySelector("summary")?.focus(); }
  });
  window.addEventListener("pageshow", (event) => { if (event.persisted && document.body.dataset.privatePage) location.reload(); });
})();
