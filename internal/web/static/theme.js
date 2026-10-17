// Runs before first paint (loaded without defer) so the chosen theme never flashes.
// Dark is the default; the choice is a per-browser convenience kept in localStorage.
(function () {
  var theme = "dark";
  try { theme = localStorage.getItem("sbx-theme") || "dark"; } catch (e) {}
  document.documentElement.setAttribute("data-theme", theme === "light" ? "light" : "dark");
})();
