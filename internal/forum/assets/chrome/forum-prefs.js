// The chrome's remembered choices (besides the theme, which forum-theme.js
// applies before first paint). Annotation mode starts On in every new page
// load unless the user switched it Off before; the choice is kept in
// localStorage, best effort: with storage blocked the default simply holds.
(function (root) {
  "use strict";
  const ANNOTATE_KEY = "forum-annotate";

  function annotate() {
    try {
      return root.localStorage.getItem(ANNOTATE_KEY) !== "off";
    } catch {
      return true; // storage blocked or unavailable
    }
  }

  function setAnnotate(on) {
    try {
      root.localStorage.setItem(ANNOTATE_KEY, on ? "on" : "off");
    } catch {
      /* the choice still applies for this page view */
    }
    return !!on;
  }

  // Status marks (the badges in the artifact on what was already sent) are On
  // unless the user hid them.
  const MARKS_KEY = "forum-marks";

  function marks() {
    try {
      return root.localStorage.getItem(MARKS_KEY) !== "off";
    } catch {
      return true;
    }
  }

  function setMarks(on) {
    try {
      root.localStorage.setItem(MARKS_KEY, on ? "on" : "off");
    } catch {
      /* the choice still applies for this page view */
    }
    return !!on;
  }

  root.forumPrefs = Object.freeze({ annotate, setAnnotate, marks, setMarks });
})(window);
