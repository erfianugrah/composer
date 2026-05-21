// Wires the mobile menu open/close behaviour. Replaces the inline onclick
// handlers that were previously in Layout.astro — those would have to be
// allowlisted via 'unsafe-inline' under a Content Security Policy.
(function () {
  function open() {
    var sidebar = document.getElementById("sidebar");
    var overlay = document.getElementById("mobile-overlay");
    if (sidebar) sidebar.classList.remove("hidden");
    if (overlay) overlay.classList.remove("hidden");
  }
  function close() {
    var sidebar = document.getElementById("sidebar");
    var overlay = document.getElementById("mobile-overlay");
    if (sidebar) sidebar.classList.add("hidden");
    if (overlay) overlay.classList.add("hidden");
  }
  document.getElementById("mobile-menu-btn")?.addEventListener("click", open);
  document.getElementById("mobile-overlay")?.addEventListener("click", close);
})();
