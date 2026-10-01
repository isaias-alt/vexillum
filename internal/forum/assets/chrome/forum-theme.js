// Theme for the forum chrome. Loaded synchronously from <head> (before the
// stylesheets paint) so the saved choice is applied before the first frame and
// there is no flash. Dark is the default and does not follow the OS: with no
// saved choice, or with storage unavailable, the chrome is dark.
(function (root) {
  "use strict";
  const KEY = "forum-theme";
  const THEMES = ["dark", "light"];

  function saved() {
    try {
      const value = root.localStorage.getItem(KEY);
      return THEMES.includes(value) ? value : "dark";
    } catch {
      return "dark"; // storage blocked or unavailable
    }
  }

  function current() {
    return root.document.documentElement.getAttribute("data-fr-theme") === "light" ? "light" : "dark";
  }

  // set applies a theme and remembers it; remembering is best effort.
  function set(theme) {
    const next = theme === "light" ? "light" : "dark";
    root.document.documentElement.setAttribute("data-fr-theme", next);
    try {
      root.localStorage.setItem(KEY, next);
    } catch {
      /* the choice still applies for this page view */
    }
    return next;
  }

  root.document.documentElement.setAttribute("data-fr-theme", saved());
  root.forumTheme = Object.freeze({ current, set, toggle: () => set(current() === "dark" ? "light" : "dark") });
})(window);
