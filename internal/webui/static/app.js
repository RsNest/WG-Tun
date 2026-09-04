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
    bindTables(document);
  });
  document.addEventListener("submit", function (ev) {
    var form = ev.target;
    if (!form || form.tagName !== "FORM" || !form.hasAttribute("data-disable-on-submit")) return;
    var btn = form.querySelector('button[type="submit"], button:not([type])');
    if (btn && !btn.disabled) btn.disabled = true;
  });

  var navCookie = "transitforge_nav";
  function setNavCollapsed(on) {
    document.body.classList.toggle("nav-collapsed", on);
    document.cookie = navCookie + "=" + (on ? "collapsed" : "expanded") + "; Path=/; Max-Age=31536000; SameSite=Lax";
    var btn = document.getElementById("nav-toggle");
    if (btn) {
      btn.setAttribute("aria-expanded", on ? "false" : "true");
      var collapse = btn.getAttribute("data-label-collapse");
      var expand = btn.getAttribute("data-label-expand");
      btn.setAttribute("aria-label", on ? expand : collapse);
      btn.setAttribute("title", on ? expand : collapse);
    }
  }
  var toggle = document.getElementById("nav-toggle");
  if (toggle) {
    toggle.addEventListener("click", function () {
      setNavCollapsed(!document.body.classList.contains("nav-collapsed"));
    });
  }

  document.addEventListener("keydown", function (ev) {
    if (ev.key !== "Escape") return;
    var close = document.querySelector("[data-panel-close]");
    if (close) {
      ev.preventDefault();
      close.click();
    }
    document.querySelectorAll("details[open].account-menu, details[open].overflow-menu").forEach(function (d) {
      d.removeAttribute("open");
    });
  });

  document.addEventListener("click", function (ev) {
    document.querySelectorAll("details[open].account-menu, details[open].overflow-menu").forEach(function (d) {
      if (!d.contains(ev.target)) d.removeAttribute("open");
    });
  });

  document.addEventListener("click", function (ev) {
    var tab = ev.target.closest("[data-tab]");
    if (!tab) return;
    var group = tab.getAttribute("data-tab-group") || "panel";
    var name = tab.getAttribute("data-tab");
    var root = tab.closest("[data-tabs]") || document;
    root.querySelectorAll('[data-tab-group="' + group + '"]').forEach(function (t) {
      t.classList.toggle("active", t === tab);
    });
    root.querySelectorAll('[data-tab-panel="' + group + '"]').forEach(function (p) {
      p.hidden = p.getAttribute("data-pane") !== name;
    });
  });

  document.addEventListener("click", function (ev) {
    var btn = ev.target.closest("[data-copy]");
    if (!btn) return;
    ev.preventDefault();
    ev.stopPropagation();
    var text = btn.getAttribute("data-copy") || "";
    if (!text || !navigator.clipboard) return;
    navigator.clipboard.writeText(text).then(function () {
      var prev = btn.getAttribute("aria-label");
      btn.setAttribute("aria-label", msg("copied", "Copied"));
      setTimeout(function () {
        if (prev) btn.setAttribute("aria-label", prev);
      }, 1200);
    });
  });

  function rowActivate(row, ev) {
    if (!row || !row.getAttribute("data-href")) return;
    if (ev && ev.target.closest("a, button, summary, input, select, label, form")) return;
    window.location.href = row.getAttribute("data-href");
  }
  document.addEventListener("click", function (ev) {
    var row = ev.target.closest("tr[data-href]");
    rowActivate(row, ev);
  });
  document.addEventListener("keydown", function (ev) {
    if (ev.key !== "Enter" && ev.key !== " ") return;
    var row = ev.target.closest("tr[data-href]");
    if (!row) return;
    ev.preventDefault();
    rowActivate(row, ev);
  });

  function bindTables(root) {
    (root.querySelectorAll ? root : document).querySelectorAll("table.data").forEach(function (table) {
      if (table.dataset.bound === "1") return;
      table.dataset.bound = "1";
      table.querySelectorAll("th[data-sort]").forEach(function (th) {
        th.addEventListener("click", function () {
          sortTable(table, th);
        });
      });
    });
  }

  function sortTable(table, th) {
    var idx = Array.prototype.indexOf.call(th.parentNode.children, th);
    var tbody = table.tBodies[0];
    if (!tbody) return;
    var rows = Array.prototype.slice.call(tbody.rows);
    var dir = th.getAttribute("data-dir") === "asc" ? "desc" : "asc";
    table.querySelectorAll("th[data-sort]").forEach(function (h) { h.removeAttribute("data-dir"); });
    th.setAttribute("data-dir", dir);
    rows.sort(function (a, b) {
      var av = (a.cells[idx] && a.cells[idx].innerText || "").trim();
      var bv = (b.cells[idx] && b.cells[idx].innerText || "").trim();
      var an = parseFloat(av.replace(/[^0-9.-]/g, ""));
      var bn = parseFloat(bv.replace(/[^0-9.-]/g, ""));
      var cmp = (!isNaN(an) && !isNaN(bn) && av.match(/[0-9]/) && bv.match(/[0-9]/)) ? an - bn : av.localeCompare(bv, undefined, { numeric: true, sensitivity: "base" });
      return dir === "asc" ? cmp : -cmp;
    });
    rows.forEach(function (r) { tbody.appendChild(r); });
  }

  function applyFilters() {
    document.querySelectorAll("[data-filter-table]").forEach(function (bar) {
      var id = bar.getAttribute("data-filter-table");
      var table = document.getElementById(id);
      if (!table || !table.tBodies[0]) return;
      var qEl = bar.querySelector("[data-filter-q]");
      var stEl = bar.querySelector("[data-filter-status]");
      var q = qEl ? qEl.value.toLowerCase() : "";
      var st = stEl ? stEl.value : "";
      Array.prototype.forEach.call(table.tBodies[0].rows, function (row) {
        var text = row.innerText.toLowerCase();
        var ok = !q || text.indexOf(q) !== -1;
        if (ok && st) ok = (row.getAttribute("data-status") || "") === st;
        row.hidden = !ok;
      });
    });
  }
  document.addEventListener("input", function (ev) {
    if (ev.target.closest("[data-filter-table]")) applyFilters();
  });
  document.addEventListener("change", function (ev) {
    if (ev.target.closest("[data-filter-table]")) applyFilters();
  });

  bindTables(document);
})();
