/*
 * Visitor Intelligence tracker. Plain script, no dependencies, no cookies.
 *
 *   <script src="https://app.example/t.js" data-site="vi_..." data-mode="opt_in" defer></script>
 *
 * data-mode is the site's consent mode:
 *   opt_in          browser IDs are created only after window.mspTrack.consent("granted")
 *   opt_out         browser IDs are created unless window.mspTrack.consent("denied")
 *   cookieless_only browser IDs are never created; only anonymous page views are sent
 * The server applies the same rule, so a page that ignores it gains nothing.
 */
(function () {
  "use strict";
  var script = document.currentScript;
  if (!script || window.mspTrack) return;
  var site = script.getAttribute("data-site");
  var mode = script.getAttribute("data-mode") || "opt_in";
  var endpoint = script.getAttribute("data-endpoint") || new URL("/api/v1/public/collect", script.src).toString();
  if (!site) return;

  var CONSENT = "msp_consent", ANON = "msp_vid", SESSION = "msp_sid";

  function store(kind) {
    try { return kind === "local" ? window.localStorage : window.sessionStorage; } catch { return null; }
  }
  function consent() {
    var s = store("local");
    return (s && s.getItem(CONSENT)) || "";
  }
  function identifiable() {
    if (mode === "cookieless_only") return false;
    if (mode === "opt_out") return consent() !== "denied";
    return consent() === "granted";
  }
  function randomId() {
    if (window.crypto && window.crypto.randomUUID) return window.crypto.randomUUID();
    return Math.random().toString(36).slice(2) + Date.now().toString(36);
  }
  function id(kind, key) {
    var s = store(kind);
    if (!s) return "";
    var v = s.getItem(key);
    if (!v) { v = randomId(); s.setItem(key, v); }
    return v;
  }
  function forget() {
    var l = store("local"), s = store("session");
    if (l) l.removeItem(ANON);
    if (s) s.removeItem(SESSION);
  }
  function send(body) {
    body.site_key = site;
    var c = consent();
    if (c) body.consent = c;
    if (identifiable()) {
      body.anonymous_id = id("local", ANON);
      body.session_id = id("session", SESSION);
    }
    var data = JSON.stringify(body);
    // text/plain needs no preflight request, and sendBeacon survives the page closing.
    if (navigator.sendBeacon && navigator.sendBeacon(endpoint, new Blob([data], { type: "text/plain" }))) return;
    fetch(endpoint, { method: "POST", body: data, keepalive: true, credentials: "omit", headers: { "Content-Type": "text/plain" } }).catch(function () {});
  }
  function pageView() {
    send({ type: "page_view", url: location.href, referrer: document.referrer });
  }

  window.mspTrack = {
    /** Records the visitor's choice. "denied" also deletes the browser IDs. */
    consent: function (state) {
      if (state !== "granted" && state !== "denied") return;
      var s = store("local");
      if (s) s.setItem(CONSENT, state);
      if (state === "denied") forget();
      send({ type: "consent", consent: state === "denied" ? "denied" : "granted" });
    },
    /** Records a named event, for custom-event goals. */
    event: function (name) { send({ type: "custom", name: String(name).slice(0, 100), url: location.href }); },
    pageView: pageView,
  };

  // Single-page apps change pages without loading; count those too.
  var push = history.pushState;
  history.pushState = function () { push.apply(this, arguments); pageView(); };
  window.addEventListener("popstate", pageView);
  pageView();
})();
