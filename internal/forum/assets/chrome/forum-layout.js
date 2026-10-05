// Passive layout audit, injected into every artifact next to the SDK.
//
// Once the page has settled it looks for failures that can be proven from the
// rendered geometry: text cut off by the box that holds it, a control the user
// cannot reach, text almost entirely covered by another element, content that
// makes the page scroll sideways. It reports them to the forum chrome, which
// files them in the "Layout issues" tray. The audit only reports: it never
// touches the artifact and nothing it finds reaches the agent by itself.
//
// Being wrong is worse than being quiet, so every finding needs evidence. A
// finding is a measured overlap of real glyph boxes or control boxes with the
// box that clips or covers them, it has to appear in two samples taken a
// moment apart, and anything hidden, mid-animation, masked, deliberately
// truncated or inside an intentional scroller is left alone.
//
// The file has two halves. The first is a set of pure classifiers over plain
// rectangles (no DOM, covered by node tests, exposed as window.forumLayout).
// The second drives them against the live document and only exists when the
// artifact runs inside the forum chrome.
(function () {
  "use strict";
  if (window.forumLayout) return;

  // ===================================================================
  // Classifiers
  // ===================================================================

  const finiteOrNull = (value) => {
    const n = Number(value);
    return Number.isFinite(n) ? n : null;
  };

  // The extent of a rect along one axis; null when it has no usable size.
  function extentOf(rect, axis) {
    if (!rect) return null;
    const across = axis === "horizontal";
    const start = finiteOrNull(across ? rect.left : rect.top);
    const end = finiteOrNull(across ? rect.right : rect.bottom);
    if (start === null || end === null) return null;
    const stated = finiteOrNull(across ? rect.width : rect.height);
    const size = Math.max(0, stated === null ? end - start : stated);
    return size > 0 ? { start, end, size } : null;
  }

  function limitsOf(box, axis) {
    if (!box) return null;
    const across = axis === "horizontal";
    const lo = finiteOrNull(across ? box.left : box.top);
    const hi = finiteOrNull(across ? box.right : box.bottom);
    return lo === null || hi === null ? null : { lo, hi };
  }

  // How far `rect` sticks out of `box` along an axis: pixels past each side,
  // the share of the rect that is outside, and whether its midpoint is.
  function protrusion(rect, box, axis) {
    const extent = extentOf(rect, axis);
    const limits = limitsOf(box, axis);
    if (!extent || !limits) return null;
    const before = Math.max(0, limits.lo - extent.start);
    const after = Math.max(0, extent.end - limits.hi);
    const middle = extent.start + extent.size / 2;
    return {
      before,
      after,
      px: Math.max(before, after),
      share: Math.min(1, (before + after) / extent.size),
      middleOut: middle < limits.lo || middle > limits.hi,
    };
  }

  // Text glyph boxes against the box that is supposed to hold them. Only a
  // container that clips counts: on the horizontal axis overflow hidden/clip,
  // on the vertical axis also visible (a fixed-height box whose text spills out
  // below it). auto/scroll is an intentional scroller. Deliberate truncation
  // and the screen-reader-only recipe are author intent.
  function classifySevereTextOverflow(input) {
    const { fragments, box, overflowX, overflowY, isTruncated = false, isVisuallyHidden = false, minOutsideRatio = 0.2, epsilon = 1 } = input || {};
    if (isTruncated || isVisuallyHidden || !box || !Array.isArray(fragments)) return null;
    const cutsAcross = overflowX === "hidden" || overflowX === "clip";
    const cutsDown = overflowY === "hidden" || overflowY === "clip" || overflowY === "visible";
    let worst = null;
    const note = (axis, px) => {
      if (!worst || px > worst.overflowPx) worst = { axis, kind: "clipped-text", overflowPx: px };
    };
    for (const fragment of fragments) {
      const across = cutsAcross ? protrusion(fragment, box, "horizontal") : null;
      if (across && across.px > epsilon && (across.middleOut || across.share >= minOutsideRatio)) note("horizontal", across.px);
      const down = cutsDown ? protrusion(fragment, box, "vertical") : null;
      if (down && down.px > epsilon && down.middleOut) note("vertical", down.px);
    }
    return worst;
  }

  // A rect leaving a boundary by a visible amount: enough pixels, and either
  // its midpoint outside or a fifth of it outside.
  function classifyMaterialRectEscape(input) {
    const { rect, boundary, axes = ["horizontal", "vertical"], minOutsidePx = 4, minOutsideRatio = 0.2 } = input || {};
    let worst = null;
    for (const axis of axes) {
      const p = protrusion(rect, boundary, axis);
      if (!p || p.px < minOutsidePx || (!p.middleOut && p.share < minOutsideRatio)) continue;
      if (!worst || p.px > worst.overflowPx) worst = { axis, side: p.before >= p.after ? "start" : "end", overflowPx: p.px };
    }
    return worst;
  }

  // A few pixels of document overshoot are cosmetic: the page only counts as
  // scrolling sideways when real content is out there and the overshoot is at
  // least 24px or 5% of the viewport.
  function isMaterialPageOverflow({ overflowPx, viewportWidth, hasEscapedContent }) {
    const overshoot = Number(overflowPx);
    const width = Number(viewportWidth);
    const floor = Math.max(24, Number.isFinite(width) ? width * 0.05 : 24);
    return Boolean(hasEscapedContent) && Number.isFinite(overshoot) && overshoot >= floor;
  }

  // Layout that is still settling is not a failure: keep what the later sample
  // shares with the earlier one.
  function findStableLayoutFindings(earlier, later) {
    const identity = (f) => [f.kind, f.selector, f.axis || ""].join(":");
    const errors = (list) => (Array.isArray(list) ? list.filter((f) => f && f.severity === "error") : []);
    const before = new Set(errors(earlier).map(identity));
    return errors(later).filter((f) => before.has(identity(f)));
  }

  function isNearTotalOcclusion({ occludedSamples, totalSamples, minSamples = 5, minRatio = 0.9 }) {
    const covered = Number(occludedSamples);
    const total = Number(totalSamples);
    return Number.isFinite(covered) && Number.isFinite(total) && total >= minSamples && covered / total >= minRatio;
  }

  const classifiers = Object.freeze({ classifySevereTextOverflow, classifyMaterialRectEscape, isMaterialPageOverflow, findStableLayoutFindings, isNearTotalOcclusion });

  // Opened directly (nothing to report to) or under a unit test (nothing to
  // look at): only the classifiers exist.
  if (window.parent === window || !window.document || !window.document.documentElement) {
    window.forumLayout = classifiers;
    return;
  }

  // ===================================================================
  // Live audit
  // ===================================================================

  const doc = window.document;

  // The artifact version this document was served as (see injectSDK). It is
  // read while the script runs: document.currentScript is gone afterwards.
  const DOC_VERSION = (() => {
    try {
      return new URL(doc.currentScript.src).searchParams.get("av") || "";
    } catch {
      return "";
    }
  })();

  const TIMING = {
    firstRun: 50, // after the script loads
    resizeDebounce: 300,
    calm: 180, // no layout/DOM change for this long counts as settled
    calmCap: 2000, // give up waiting for calm after this long
    animationCap: 4000,
    resample: 120, // gap between the two samples
    frameFallback: 120, // a hidden tab never paints; do not wait for it
  };
  const LIMITS = { elements: 800, findings: 100, coverCandidates: 200, coverMinText: 8 };

  const selectorFor = (el) => (window.forum && window.forum.__dom ? window.forum.__dom.selectorOf(el) : "");
  const squash = (text) => String(text || "").trim().replace(/\s+/g, " ");
  const toPx = (value) => {
    const n = Number.parseFloat(String(value || "0"));
    return Number.isFinite(n) ? n : 0;
  };
  const tenths = (n) => Math.round(Math.max(0, n) * 10) / 10;
  const isRoot = (el) => !el || el === doc.body || el === doc.documentElement;

  const CLIPS = new Set(["hidden", "clip"]);
  const SCROLLS = new Set(["auto", "scroll"]);
  const PLACED = new Set(["absolute", "fixed", "sticky"]);

  // Elements that answer for the text inside them, descendants included. Any
  // other element answers only for its own direct text nodes.
  const TEXT_BLOCKS =
    "p,h1,h2,h3,h4,h5,h6,button,label,a[href],li,dt,dd,th,td,legend,figcaption,summary,[role='button'],[role='link'],[role='alert'],[role='status']";
  const CONTROLS = "button,input,select,textarea,a[href],summary,[role]";
  const CONTROL_ROLES = new Set(["button", "link", "checkbox", "radio", "switch", "textbox", "combobox"]);

  // A control the user has to be able to use.
  function isControl(el) {
    if (!el.matches || !el.matches(CONTROLS)) return false;
    if (el.matches("input[type='hidden'],[disabled],[aria-disabled='true']")) return false;
    const role = el.getAttribute("role");
    return role === null || CONTROL_ROLES.has(role.toLowerCase());
  }

  // ---- style predicates -------------------------------------------------

  const truncatesOnPurpose = (style) => style.textOverflow === "ellipsis" || Number.parseInt(style.webkitLineClamp || style.lineClamp || "0", 10) > 0;

  const masksContent = (style) => {
    const mask = String(style.maskImage || style.webkitMaskImage || "none").toLowerCase();
    const path = String(style.clipPath || "none").toLowerCase();
    return (mask !== "none" && mask !== "") || (path !== "none" && path !== "");
  };

  // Rounded corners with overflow clipping trim the corners on purpose; the
  // whole subtree is treated like masked content.
  const clipsRounded = (style) => {
    if (!CLIPS.has(style.overflowX) && !CLIPS.has(style.overflowY)) return false;
    return [style.borderTopLeftRadius, style.borderTopRightRadius, style.borderBottomRightRadius, style.borderBottomLeftRadius].some((r) => toPx(r) > 0);
  };

  // The usual screen-reader-only recipe: a tiny, positioned, clipped box.
  function looksScreenReaderOnly(style, rect) {
    if (!PLACED.has(style.position) || !CLIPS.has(style.overflowX) || rect.width > 2 || rect.height > 2) return false;
    const legacyClip = String(style.clip || "").toLowerCase();
    const path = String(style.clipPath || "").toLowerCase();
    return style.whiteSpace === "nowrap" || legacyClip !== "auto" || (path !== "none" && path !== "");
  }

  // Alpha of a computed colour: 0..1, or null when the format is not one we read.
  function alphaOf(color) {
    const value = String(color || "").trim().toLowerCase();
    if (value === "" || value === "transparent") return 0;
    const match = value.match(/^rgba?\(([^)]+)\)$/);
    if (!match) return null;
    const parts = match[1].split(/[\s,/]+/).filter(Boolean);
    return parts.length < 4 ? 1 : Number(parts[3]);
  }

  // ---- the scene: memoized facts about one sample ------------------------

  function liveAnimations() {
    if (typeof doc.getAnimations !== "function") return [];
    return doc.getAnimations().filter((a) => a.playState === "running" || a.playState === "pending");
  }

  function animationTarget(animation) {
    const target = animation.effect && animation.effect.target;
    if (target instanceof Element) return target;
    return target && target.element instanceof Element ? target.element : null;
  }

  // Everything one sample needs to know about the document. Styles, boxes and
  // text are read at most once per element, and "is any ancestor X" questions
  // are answered by walking up once and remembering the result per node.
  function openScene() {
    const root = doc.documentElement;
    const view = { width: window.innerWidth || root.clientWidth || 0, height: window.innerHeight || 0 };
    const viewBox = { left: 0, right: view.width, top: 0, bottom: view.height };
    const caches = { style: new Map(), rect: new Map(), pad: new Map(), text: new Map(), glyphs: new Map() };
    const once = (cache, key, make) => {
      if (!cache.has(key)) cache.set(key, make());
      return cache.get(key);
    };
    const style = (el) => once(caches.style, el, () => window.getComputedStyle(el));
    const rect = (el) => once(caches.rect, el, () => el.getBoundingClientRect());

    // inherited(test)(el): does `test` hold for el or any ancestor?
    const inherited = (test) => {
      const memo = new Map();
      const walk = (el) => {
        if (!el || el.nodeType !== 1) return false;
        if (!memo.has(el)) memo.set(el, test(el) || walk(el.parentElement));
        return memo.get(el);
      };
      return walk;
    };

    const gone = inherited((el) => {
      const s = style(el);
      const opacity = Number.parseFloat(s.opacity || "1");
      return s.display === "none" || s.contentVisibility === "hidden" || (Number.isFinite(opacity) && opacity <= 0.01);
    });
    // Parts of the document the audit never judges: the forum's own overlay,
    // diagrams that lay themselves out, masked content, screen-reader-only text.
    const setAside = inherited((el) => {
      if (el.matches(".mermaid,svg,[data-forum-ui]")) return true;
      const s = style(el);
      return masksContent(s) || clipsRounded(s) || looksScreenReaderOnly(s, rect(el));
    });
    const scrollsAcross = inherited((el) => !isRoot(el) && SCROLLS.has(style(el).overflowX));
    const scrollsDown = inherited((el) => !isRoot(el) && SCROLLS.has(style(el).overflowY));
    const underTextBlock = inherited((el) => el.matches(TEXT_BLOCKS));

    // Anything with a running animation (or an ancestor/descendant of it) moves
    // on purpose.
    const animated = liveAnimations().map(animationTarget).filter((el) => el && !el.closest("[data-forum-ui]"));
    const moving = (el) => animated.some((t) => t === el || t.contains(el) || el.contains(t));

    const isBlock = (el) => el.matches(TEXT_BLOCKS);

    // The text this element answers for ("" when an enclosing block owns it).
    const textOf = (el) =>
      once(caches.text, el, () => {
        if (isBlock(el)) return squash(el.innerText || el.textContent);
        if (underTextBlock(el.parentElement)) return "";
        return squash([...el.childNodes].filter((n) => n.nodeType === 3).map((n) => n.textContent).join(" "));
      });

    // Client rects of the text nodes the element answers for.
    const glyphs = (el) =>
      once(caches.glyphs, el, () => {
        const nodes = [];
        if (isBlock(el)) {
          const walker = doc.createTreeWalker(el, 4); // SHOW_TEXT
          for (let n = walker.nextNode(); n; n = walker.nextNode()) nodes.push(n);
        } else {
          nodes.push(...[...el.childNodes].filter((n) => n.nodeType === 3));
        }
        const out = [];
        for (const node of nodes) {
          if (!String(node.textContent || "").trim()) continue;
          const range = doc.createRange();
          range.selectNodeContents(node);
          for (const r of range.getClientRects()) if (r.width > 0 && r.height > 0) out.push(r);
          if (range.detach) range.detach();
        }
        return out;
      });

    const padBox = (el) =>
      once(caches.pad, el, () => {
        const r = rect(el);
        const s = style(el);
        return { left: r.left + toPx(s.borderLeftWidth), right: r.right - toPx(s.borderRightWidth), top: r.top + toPx(s.borderTopWidth), bottom: r.bottom - toPx(s.borderBottomWidth) };
      });

    // Ancestors that clip their content (overflow hidden/clip), nearest first.
    // Masked and rounded ones never get here: their subtree is set aside.
    const clippers = (el) => {
      const out = [];
      for (let n = el.parentElement; n && !isRoot(n); n = n.parentElement) {
        const s = style(n);
        const axes = [];
        if (CLIPS.has(s.overflowX)) axes.push("horizontal");
        if (CLIPS.has(s.overflowY)) axes.push("vertical");
        if (axes.length > 0) out.push({ el: n, axes, box: padBox(n), truncates: truncatesOnPurpose(s) });
      }
      return out;
    };

    const rendered = (el) => {
      const r = rect(el);
      return r.width > 0 && r.height > 0 && !gone(el) && style(el).visibility !== "hidden";
    };

    return {
      view,
      viewBox,
      root,
      style,
      rect,
      padBox,
      textOf,
      glyphs,
      clippers,
      scrollsAcross,
      scrollsDown,
      // Fit to be judged: it renders, is not set aside and is not animating.
      auditable: (el) => !setAside(el) && !moving(el) && rendered(el),
      moving,
      lockedVertically: () => [root, doc.body].some((n) => n && CLIPS.has(style(n).overflowY)),
      lockedHorizontally: () => [root, doc.body].some((n) => n && CLIPS.has(style(n).overflowX)),
      elements: () => [...(doc.body ? doc.body.querySelectorAll("*") : [])].filter((el) => el instanceof Element && !el.closest("[data-forum-ui]")).slice(0, LIMITS.elements),
    };
  }

  // ---- findings ----------------------------------------------------------

  // Collects findings of one sample. Elements inside a container that already
  // has a finding are not reported again.
  function openLedger() {
    const items = [];
    const seen = new Set();
    const roots = [];
    return {
      items,
      add(kind, selector, axis, px) {
        const direction = axis === "vertical" ? "vertical" : "horizontal";
        const key = [kind, selector, direction].join(":");
        if (items.length >= LIMITS.findings || seen.has(key)) return;
        seen.add(key);
        items.push({ selector, kind, axis: direction, overflowPx: tenths(px), severity: "error" });
      },
      claim: (el) => roots.push(el),
      covers: (el) => roots.some((r) => r.contains(el)),
    };
  }

  // ---- rules ---------------------------------------------------------------

  const materialPx = (scene) => Math.max(24, scene.view.width * 0.05);
  const pastEdge = (rect, scene, minPx, side) => {
    const hit = classifyMaterialRectEscape({ rect, boundary: scene.viewBox, axes: ["horizontal"], minOutsidePx: minPx });
    return hit && hit.side === side ? hit : null;
  };

  // Does a painted, in-flow box (a fill, a border, an image) run past the right
  // edge with nothing clipping or scrolling it?
  function paintsPastRight(scene, el) {
    const s = scene.style(el);
    if (PLACED.has(s.position)) return false;
    const paints =
      el.matches("img,video,canvas,table,iframe") ||
      alphaOf(s.backgroundColor) !== 0 ||
      (s.backgroundImage && s.backgroundImage !== "none") ||
      ["Top", "Right", "Bottom", "Left"].some((side) => toPx(s["border" + side + "Width"]) > 0 && s["border" + side + "Style"] !== "none");
    if (!paints || scene.clippers(el).some((c) => c.axes.includes("horizontal"))) return false;
    return !!pastEdge(scene.rect(el), scene, materialPx(scene), "end");
  }

  // Is meaningful content sitting past the right edge of the viewport?
  function reachesPastRight(scene, el) {
    if (isRoot(el) || scene.scrollsAcross(el) || !scene.auditable(el)) return false;
    const placed = PLACED.has(scene.style(el).position);
    if (isControl(el) && pastEdge(scene.rect(el), scene, 4, "end")) return true;
    if (!placed && scene.textOf(el) && scene.glyphs(el).some((g) => pastEdge(g, scene, materialPx(scene), "end"))) return true;
    return paintsPastRight(scene, el);
  }

  function auditPageWidth(scene, ledger, elements) {
    const overshoot = scene.root.scrollWidth - scene.view.width;
    const worthChecking = isMaterialPageOverflow({ overflowPx: overshoot, viewportWidth: scene.view.width, hasEscapedContent: true });
    if (!worthChecking || scene.lockedHorizontally()) return;
    if (elements.some((el) => reachesPastRight(scene, el))) ledger.add("page-horizontal-overflow", "html", "horizontal", overshoot);
  }

  function auditControls(scene, ledger, elements) {
    for (const el of elements) {
      if (!isControl(el) || !scene.auditable(el)) continue;
      const box = scene.rect(el);

      // Cut by a clipping ancestor: report the container, once.
      let cut = null;
      for (const c of scene.clippers(el)) {
        const hit = classifyMaterialRectEscape({ rect: box, boundary: c.box, axes: c.axes });
        if (hit && (!cut || hit.overflowPx > cut.hit.overflowPx)) cut = { c, hit };
      }
      if (cut && !ledger.covers(cut.c.el)) {
        ledger.claim(cut.c.el);
        ledger.add("clipped-control", selectorFor(cut.c.el), cut.hit.axis, cut.hit.overflowPx);
      }

      // Left of the viewport: there is no scrolling there.
      if (!scene.scrollsAcross(el)) {
        const left = pastEdge(box, scene, 4, "start");
        if (left) ledger.add("viewport-unreachable-control", selectorFor(el), "horizontal", left.overflowPx);
      }

      // Above or below everything it could be scrolled into. A control inside a
      // vertical scroller is reached by scrolling that scroller.
      if (scene.scrollsDown(el.parentElement)) continue;
      const position = scene.style(el).position;
      const pinned = position === "fixed" || position === "sticky" || scene.lockedVertically();
      const lift = pinned ? 0 : Number(window.scrollY || window.pageYOffset || 0);
      const hit = classifyMaterialRectEscape({
        rect: { top: box.top + lift, bottom: box.bottom + lift, height: box.height },
        boundary: { top: 0, bottom: pinned ? scene.view.height : scene.root.scrollHeight },
        axes: ["vertical"],
      });
      if (hit) ledger.add("viewport-unreachable-control", selectorFor(el), "vertical", hit.overflowPx);
    }
  }

  // Text that sits left of the viewport can never be scrolled to.
  function auditStrandedText(scene, ledger, elements) {
    for (const el of elements) {
      if (!scene.textOf(el) || scene.scrollsAcross(el) || !scene.auditable(el)) continue;
      if (PLACED.has(scene.style(el).position) && !isControl(el)) continue;
      let worst = null;
      for (const g of scene.glyphs(el)) {
        const hit = pastEdge(g, scene, materialPx(scene), "start");
        if (hit && (!worst || hit.overflowPx > worst.overflowPx)) worst = hit;
      }
      if (worst) ledger.add("viewport-unreachable-content", selectorFor(el), "horizontal", worst.overflowPx);
    }
  }

  // Text crossing the box that clips it: its own, or any clipping ancestor's.
  function auditCutText(scene, ledger, elements) {
    for (const el of elements) {
      if (!scene.textOf(el) || ledger.covers(el) || !scene.auditable(el)) continue;
      const own = scene.style(el);
      const fragments = scene.glyphs(el);
      const deliberate = truncatesOnPurpose(own);
      let verdict = classifySevereTextOverflow({ fragments, box: scene.padBox(el), overflowX: own.overflowX, overflowY: own.overflowY, isTruncated: deliberate });
      let culprit = el;
      for (const c of scene.clippers(el)) {
        const hit = classifySevereTextOverflow({
          fragments,
          box: c.box,
          overflowX: c.axes.includes("horizontal") ? "hidden" : "auto",
          overflowY: c.axes.includes("vertical") ? "hidden" : "auto",
          isTruncated: deliberate || c.truncates,
        });
        if (hit && (!verdict || hit.overflowPx > verdict.overflowPx)) {
          verdict = hit;
          culprit = c.el;
        }
      }
      if (!verdict) continue;
      ledger.claim(culprit);
      ledger.add(verdict.kind, selectorFor(culprit), verdict.axis, verdict.overflowPx);
    }
  }

  // The branch of the tree, painted over `point`, that is a sibling of one of
  // el's own ancestors and opaque enough to hide el. Null when nothing of the
  // kind covers the point.
  function coveringBranch(scene, el, point) {
    const hit = doc.elementFromPoint(point.x, point.y);
    if (!(hit instanceof Element) || el.contains(hit) || hit.closest("[data-forum-ui]")) return null;
    const own = new Set();
    for (let n = el; n; n = n.parentElement) own.add(n);
    let opaque = false;
    let see = 1; // how much of the branch shows through, multiplied up the way
    for (let n = hit; n && !own.has(n); n = n.parentElement) {
      if (scene.moving(n)) return null;
      const s = scene.style(n);
      const opacity = Number.parseFloat(s.opacity || "1");
      if (Number.isFinite(opacity)) see *= opacity;
      const alpha = alphaOf(s.backgroundColor);
      if (opacity >= 0.95 && alpha !== null && alpha >= 0.95) opaque = true;
      if (own.has(n.parentElement)) return opaque && see >= 0.95 ? n : null;
    }
    return null;
  }

  function auditCoveredText(scene, ledger, elements) {
    let examined = 0;
    for (const el of elements) {
      if (examined >= LIMITS.coverCandidates) break;
      const text = scene.textOf(el);
      if (text.length < LIMITS.coverMinText && !(text.length > 0 && isControl(el))) continue;
      if (ledger.covers(el) || scene.style(el).position === "fixed" || !scene.auditable(el)) continue;
      examined += 1;
      const covering = new Map();
      let sampled = 0;
      for (const g of scene.glyphs(el)) {
        if (g.width * g.height < 16) continue;
        for (const fx of [0.2, 0.5, 0.8]) {
          for (const fy of [0.2, 0.5, 0.8]) {
            const point = { x: g.left + g.width * fx, y: g.top + g.height * fy };
            if (point.x < 0 || point.y < 0 || point.x > scene.view.width || point.y > scene.view.height) continue;
            sampled += 1;
            const branch = coveringBranch(scene, el, point);
            if (branch) covering.set(branch, (covering.get(branch) || 0) + 1);
          }
        }
      }
      const covered = Math.max(0, ...covering.values());
      if (!isNearTotalOcclusion({ occludedSamples: covered, totalSamples: sampled })) continue;
      ledger.claim(el);
      ledger.add("overlapping-text", selectorFor(el), "horizontal", 0);
    }
  }

  // One sample of the whole document. Order matters a little: controls and cut
  // text claim containers first so the same container is not reported twice.
  function takeSample() {
    const scene = openScene();
    const ledger = openLedger();
    const elements = scene.elements();
    auditPageWidth(scene, ledger, elements);
    auditControls(scene, ledger, elements);
    auditStrandedText(scene, ledger, elements);
    auditCutText(scene, ledger, elements);
    auditCoveredText(scene, ledger, elements);
    return ledger.items;
  }

  // ===================================================================
  // Settling, scheduling, reporting
  // ===================================================================

  const sleep = (ms) => new Promise((resolve) => window.setTimeout(resolve, ms));

  function paintOnce() {
    return new Promise((resolve) => {
      let done = false;
      const go = () => {
        if (!done) {
          done = true;
          resolve();
        }
      };
      if (typeof window.requestAnimationFrame === "function") window.requestAnimationFrame(go);
      window.setTimeout(go, TIMING.frameFallback);
    });
  }

  async function paintFrames(count) {
    for (let i = 0; i < count; i += 1) await paintOnce();
  }

  function fontsReady() {
    const fonts = doc.fonts;
    try {
      if (fonts && fonts.ready) return fonts.ready.catch(() => {});
    } catch {
      /* the calm wait below is still a safety net */
    }
    return Promise.resolve();
  }

  // Resolves true once `attach`'s signal has been quiet for TIMING.calm, or
  // false when TIMING.calmCap runs out first. attach(poke) starts watching,
  // calls poke() on every change and returns a function that stops watching.
  function untilCalm(attach) {
    return new Promise((resolve) => {
      let idle = 0;
      let cap = 0;
      let over = false;
      let stop = () => {};
      const end = (calm) => {
        if (over) return;
        over = true;
        window.clearTimeout(idle);
        window.clearTimeout(cap);
        stop();
        resolve(calm);
      };
      const poke = () => {
        window.clearTimeout(idle);
        idle = window.setTimeout(() => end(true), TIMING.calm);
      };
      stop = attach(poke) || stop;
      poke();
      cap = window.setTimeout(() => end(false), TIMING.calmCap);
    });
  }

  const watchLayout = (poke) => {
    if (typeof ResizeObserver === "undefined") return null;
    const observer = new ResizeObserver(poke);
    const targets = [doc.documentElement, doc.body, ...(doc.body ? doc.body.querySelectorAll("*") : [])].filter(Boolean).slice(0, LIMITS.elements);
    for (const el of targets) observer.observe(el);
    return () => observer.disconnect();
  };

  // A document that keeps mutating (a client framework still hydrating) is not
  // ready to be judged; without the observer there is no way to tell.
  const settledDom = () =>
    typeof MutationObserver === "undefined"
      ? Promise.resolve(false)
      : untilCalm((poke) => {
          const observer = new MutationObserver(poke);
          observer.observe(doc.documentElement, { attributes: true, characterData: true, childList: true, subtree: true });
          return () => observer.disconnect();
        });

  // Waits for finite animations to end. Infinite ones keep running: the audit
  // just ignores what they animate. Returns false when some are still going,
  // and the audit runs again when they finish.
  async function finiteAnimationsDone() {
    const finite = liveAnimations().filter((a) => {
      const timing = a.effect && a.effect.getComputedTiming && a.effect.getComputedTiming();
      return timing && Number.isFinite(Number(timing.endTime));
    });
    if (finite.length === 0) return true;
    let done = false;
    await Promise.race([
      Promise.all(finite.map((a) => a.finished.catch(() => {}))).then(() => {
        done = true;
      }),
      sleep(TIMING.animationCap),
    ]);
    if (!done) for (const a of finite) a.finished.then(requestAudit, requestAudit);
    return done;
  }

  let generation = 0;
  let pending = 0;
  let lastSent = null;

  // A pass says how complete it is: an incomplete pass is uncertainty, never
  // proof that an earlier finding went away.
  function send(findings, complete, presenceComplete) {
    const width = window.innerWidth || doc.documentElement.clientWidth || 0;
    const errors = findings.filter((f) => f && f.severity === "error");
    const signature = JSON.stringify([complete, presenceComplete, width, errors]);
    if (signature === lastSent) return;
    lastSent = signature;
    window.parent.postMessage(
      {
        type: "forum:layout",
        artifact_version: DOC_VERSION,
        complete,
        target_presence_complete: presenceComplete === true,
        viewport_width: width,
        findings: errors.map((f) => ({ kind: f.kind, selector: f.selector, axis: f.axis, overflow_px: f.overflowPx })),
      },
      "*",
    );
  }

  async function auditOnce(id) {
    await fontsReady();
    await untilCalm(watchLayout);
    const animationsDone = await finiteAnimationsDone();
    await paintFrames(2);
    if (id !== generation) return;
    const samples = [takeSample()];
    await sleep(TIMING.resample);
    await paintFrames(2);
    if (id !== generation) return;
    samples.push(takeSample());
    const hydrated = await settledDom();
    if (id !== generation) return;
    if (hydrated) samples.push(takeSample());
    const presenceComplete = doc.readyState === "complete" && hydrated;
    const stable = findStableLayoutFindings(samples[samples.length - 2], samples[samples.length - 1]);
    send(stable, animationsDone && presenceComplete, presenceComplete);
  }

  // Starting an audit supersedes any audit still in flight.
  function requestAudit() {
    window.clearTimeout(pending);
    const id = ++generation;
    pending = window.setTimeout(() => {
      auditOnce(id).catch(() => {
        if (id === generation) send([], false, false);
      });
    }, TIMING.firstRun);
  }

  // Only what can change layout re-runs the audit: the first settle, the load
  // event, and the end of a resize. animationend and transitionend are not
  // listened to (they bubble from every hover and fade in the document);
  // finite animations re-run it themselves when they finish.
  let resizing = 0;
  window.addEventListener("load", requestAudit, { once: true });
  window.addEventListener(
    "resize",
    () => {
      window.clearTimeout(resizing);
      resizing = window.setTimeout(requestAudit, TIMING.resizeDebounce);
    },
    { passive: true },
  );
  requestAudit();

  window.forumLayout = classifiers;
})();
