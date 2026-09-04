(function () {
  function banner() {
    return document.getElementById("dash-refresh-error");
  }
  function hxError() {
    return document.getElementById("hx-error");
  }
  function msg(name, fallback) {
    var v = document.body && document.body.getAttribute("data-i18n-" + name);
    return v || fallback;
  }
  function show(el, text) {
    if (!el) return;
    if (text) el.textContent = text;
    el.hidden = false;
  }
  function hide(el) {
    if (!el) return;
    el.hidden = true;
  }
  function isDashCards(elt) {
    return elt && (elt.id === "node-cards" || (elt.closest && elt.closest("#node-cards")));
  }
  document.body.addEventListener("htmx:sendError", function (ev) {
    if (isDashCards(ev.detail && ev.detail.elt)) {
      show(banner(), msg("refresh-fail", "Could not refresh node status. Showing last successful data."));
      return;
    }
    show(hxError(), msg("no-response", "The controller did not respond."));
  });
  document.body.addEventListener("htmx:responseError", function (ev) {
    var xhr = ev.detail && ev.detail.xhr;
    var status = xhr ? xhr.status : 0;
    if (isDashCards(ev.detail && ev.detail.elt)) {
      show(banner(), msg("refresh-fail", "Could not refresh node status. Showing last successful data."));
      return;
    }
    var text = msg("request-failed", "Request failed.");
    if (status === 401) text = msg("session", "Session expired. Sign in again.");
    else if (status === 403) text = msg("forbidden", "This action requires the operator role.");
    else if (status === 404) text = msg("not-found", "The requested resource was not found.");
    else if (status === 409) text = msg("conflict", "The controller reported a conflict.");
    else if (status === 422 || status === 400) text = msg("validation", "The submitted values were not accepted.");
    else if (status >= 500) text = msg("controller", "The controller could not complete this request.");
    show(hxError(), text);
  });
  document.body.addEventListener("htmx:afterSwap", function (ev) {
    if (isDashCards(ev.detail && ev.detail.target)) hide(banner());
    hide(hxError());
  });
  document.addEventListener("submit", function (ev) {
    var form = ev.target;
    if (!form || form.tagName !== "FORM" || !form.hasAttribute("data-disable-on-submit")) return;
    var btn = form.querySelector('button[type="submit"], button:not([type])');
    if (btn && !btn.disabled) btn.disabled = true;
  });
})();
