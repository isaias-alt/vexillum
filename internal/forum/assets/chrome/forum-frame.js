// Starts the artifact loading, right after its iframe is parsed and before
// the rest of the chrome runs. The iframe has no src in the markup: the theme
// is part of the artifact's address, so the server renders the artifact in the
// chrome's current theme from the first paint (an iframe cannot read the
// chrome's storage, and a message after load would flash the wrong theme).
(function () {
  "use strict";
  const frame = document.getElementById("artifact");
  frame.src = frame.dataset.src + "?theme=" + window.forumTheme.current();
})();
