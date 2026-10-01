// Passive layout diagnostics, injected into every artifact next to the SDK.
// After the page settles it looks for severe, provable layout failures - text
// cut off by its container, a control the user cannot reach, text covered by
// another element, the page scrolling sideways - and reports them to the
// forum chrome, which files them in the "Layout issues" tray. It only reports:
// nothing here reaches the agent, and the artifact is never modified.
//
// The classifiers and the audit are adapted from upstream's
// artifact-sdk.js (MIT, v0.1.80); see THIRD-PARTY-NOTICES.md at the vexillum
// repo root. They are deliberately conservative: a finding needs rendered
// proof (a real text fragment crossing its own clipping box, a sampled point
// covered by an opaque sibling), it must be present in two samples taken a
// moment apart, and anything hidden, animated, masked, deliberately truncated
// or inside an intentional scroller stays silent.
(function () {
  "use strict";
  if (window.forumLayout) return;

  // ---------------------------------------------------------------- classifiers
  // Pure functions over plain rects: no DOM, covered by node tests.

  // classifySevereTextOverflow: text fragments crossing the clipping box they
  // live in. Explicit truncation (ellipsis, line-clamp) and standard
  // screen-reader-only hiding are author intent and stay silent.
  function classifySevereTextOverflow({ fragments, box, overflowX, overflowY, isTruncated = false, isVisuallyHidden = false, minOutsideRatio = 0.2, epsilon = 1 }) {
    function overflowOf(fragment, boundary, axis) {
      const horizontal = axis === "horizontal";
      const start = Number(horizontal ? fragment.left : fragment.top);
      const end = Number(horizontal ? fragment.right : fragment.bottom);
      const boxStart = Number(horizontal ? boundary.left : boundary.top);
      const boxEnd = Number(horizontal ? boundary.right : boundary.bottom);
      const explicitSize = Number(horizontal ? fragment.width : fragment.height);
      const size = Number.isFinite(explicitSize) ? Math.max(0, explicitSize) : Math.max(0, end - start);
      if (![start, end, boxStart, boxEnd, size].every(Number.isFinite) || size <= 0) return { overflowPx: 0, outsideRatio: 0, centerOutside: false };
      const before = Math.max(0, boxStart - start);
      const after = Math.max(0, end - boxEnd);
      const center = start + size / 2;
      return { overflowPx: Math.max(before, after), outsideRatio: Math.min(1, (before + after) / size), centerOutside: center < boxStart || center > boxEnd };
    }

    if (isTruncated || isVisuallyHidden || !box || !Array.isArray(fragments) || fragments.length === 0) return null;
    const clipsX = overflowX === "hidden" || overflowX === "clip";
    const clipsY = overflowY === "hidden" || overflowY === "clip";
    const spillsY = overflowY === "visible";
    const scrollsX = overflowX === "auto" || overflowX === "scroll";
    const scrollsY = overflowY === "auto" || overflowY === "scroll";
    let strongest = null;
    for (const fragment of fragments) {
      const horizontal = overflowOf(fragment, box, "horizontal");
      const vertical = overflowOf(fragment, box, "vertical");
      const severeX = clipsX && !scrollsX && horizontal.overflowPx > epsilon && (horizontal.centerOutside || horizontal.outsideRatio >= minOutsideRatio);
      const severeY = (clipsY || spillsY) && !scrollsY && vertical.overflowPx > epsilon && vertical.centerOutside;
      for (const candidate of [severeX ? { axis: "horizontal", kind: "clipped-text", overflowPx: horizontal.overflowPx } : null, severeY ? { axis: "vertical", kind: "clipped-text", overflowPx: vertical.overflowPx } : null]) {
        if (candidate && (!strongest || candidate.overflowPx > strongest.overflowPx)) strongest = candidate;
      }
    }
    return strongest;
  }

  // classifyMaterialRectEscape: a rect leaving a boundary by enough pixels, and
  // either with its center outside or with a fifth of it outside.
  function classifyMaterialRectEscape({ rect, boundary, axes = ["horizontal", "vertical"], minOutsidePx = 4, minOutsideRatio = 0.2 }) {
    let strongest = null;
    for (const axis of axes) {
      const horizontal = axis === "horizontal";
      const start = Number(horizontal ? rect && rect.left : rect && rect.top);
      const end = Number(horizontal ? rect && rect.right : rect && rect.bottom);
      const boundaryStart = Number(horizontal ? boundary && boundary.left : boundary && boundary.top);
      const boundaryEnd = Number(horizontal ? boundary && boundary.right : boundary && boundary.bottom);
      const explicitSize = Number(horizontal ? rect && rect.width : rect && rect.height);
      const size = Number.isFinite(explicitSize) ? Math.max(0, explicitSize) : Math.max(0, end - start);
      if (![start, end, boundaryStart, boundaryEnd, size].every(Number.isFinite) || size <= 0) continue;
      const before = Math.max(0, boundaryStart - start);
      const after = Math.max(0, end - boundaryEnd);
      const outsidePx = Math.max(before, after);
      const outsideRatio = Math.min(1, (before + after) / size);
      const center = start + size / 2;
      const centerOutside = center < boundaryStart || center > boundaryEnd;
      if (outsidePx < minOutsidePx || (!centerOutside && outsideRatio < minOutsideRatio)) continue;
      const candidate = { axis, side: before >= after ? "start" : "end", overflowPx: outsidePx };
      if (!strongest || candidate.overflowPx > strongest.overflowPx) strongest = candidate;
    }
    return strongest;
  }

  // Tiny document deltas are cosmetic: the page only counts as overflowing
  // when meaningful content really escapes the viewport.
  function isMaterialPageOverflow({ overflowPx, viewportWidth, hasEscapedContent }) {
    const overflow = Number(overflowPx);
    const width = Number(viewportWidth);
    return Boolean(hasEscapedContent) && Number.isFinite(overflow) && overflow >= Math.max(24, Number.isFinite(width) ? width * 0.05 : 24);
  }

  // A finding must show up in both samples: layout still settling is not a failure.
  function findStableLayoutFindings(first, second) {
    const key = (finding) => finding.kind + ":" + finding.selector + ":" + (finding.axis || "");
    const firstKeys = new Set((Array.isArray(first) ? first : []).filter((f) => f && f.severity === "error").map(key));
    return (Array.isArray(second) ? second : []).filter((f) => f && f.severity === "error" && firstKeys.has(key(f)));
  }

  function isNearTotalOcclusion({ occludedSamples, totalSamples, minSamples = 5, minRatio = 0.9 }) {
    const occluded = Number(occludedSamples);
    const total = Number(totalSamples);
    return Number.isFinite(occluded) && Number.isFinite(total) && total >= minSamples && occluded / total >= minRatio;
  }

  const api = { classifySevereTextOverflow, classifyMaterialRectEscape, isMaterialPageOverflow, findStableLayoutFindings, isNearTotalOcclusion };

  // Without a forum chrome to report to (the artifact opened directly) or a
  // document to look at (the unit tests), only the classifiers exist.
  if (window.parent === window || !window.document || !window.document.documentElement) {
    window.forumLayout = Object.freeze(api);
    return;
  }

  // -------------------------------------------------------------------- audit

  // The artifact version this document was served from (see injectSDK).
  const DOC_VERSION = (() => {
    try {
      return new URL(document.currentScript.src).searchParams.get("av") || "";
    } catch {
      return "";
    }
  })();

  const RESIZE_DEBOUNCE_MS = 300;
  const SETTLE_MS = 180;
  const MAX_WAIT_MS = 2000;
  const ANIMATION_MAX_WAIT_MS = 4000;
  const STABLE_SAMPLE_MS = 120;
  const MAX_ELEMENTS = 800;
  const MAX_FINDINGS = 100;

  let auditTimer = 0;
  let auditRun = 0;
  let lastSignature = null;

  const selectorOf = (el) => (window.forum && window.forum.__dom ? window.forum.__dom.selectorOf(el) : "");
  const toPx = (value) => {
    const parsed = Number.parseFloat(String(value || "0"));
    return Number.isFinite(parsed) ? parsed : 0;
  };
  const roundPx = (value) => Math.round(Math.max(0, value) * 10) / 10;
  const rectArea = (rect) => Math.max(0, rect.width) * Math.max(0, rect.height);

  // The forum's own overlay (annotation rings) is never audited.
  const isForumUi = (el) => !!(el && el.closest && el.closest("[data-forum-ui]"));

  function elementText(el) {
    return String((el && (el.innerText || el.textContent)) || "").trim().replace(/\s+/g, " ");
  }

  function directText(el) {
    return [...((el && el.childNodes) || [])]
      .filter((node) => node.nodeType === 3)
      .map((node) => String(node.textContent || ""))
      .join(" ")
      .trim()
      .replace(/\s+/g, " ");
  }

  const CONTROL_ROLES = new Set(["button", "link", "checkbox", "radio", "switch", "textbox", "combobox"]);

  // A control the user must be able to use: not hidden, not disabled.
  function isRequiredControl(el) {
    if (!el.matches || !el.matches("button,input,select,textarea,a[href],summary,[role]")) return false;
    if (el.matches("input[type='hidden'],[disabled],[aria-disabled='true']")) return false;
    if (!el.hasAttribute("role")) return true;
    return CONTROL_ROLES.has(String(el.getAttribute("role") || "").toLowerCase());
  }

  const TEXT_BOUNDARY = "p,h1,h2,h3,h4,h5,h6,button,label,a[href],li,dt,dd,th,td,legend,figcaption,summary,[role='button'],[role='link'],[role='alert'],[role='status']";
  const isSemanticTextBoundary = (el) => !!(el && el.matches && el.matches(TEXT_BOUNDARY));

  function hasSemanticTextBoundaryAncestor(el) {
    for (let node = el && el.parentElement; node && node !== document.body && node !== document.documentElement; node = node.parentElement) {
      if (isSemanticTextBoundary(node)) return true;
    }
    return false;
  }

  const auditedText = (el) => (isSemanticTextBoundary(el) ? elementText(el) : directText(el));

  // Visible means it renders: a box, not display:none / visibility:hidden /
  // content-visibility:hidden / fully transparent, anywhere up the tree.
  function isVisible(el, rect = el.getBoundingClientRect()) {
    if (!el || isForumUi(el) || rect.width <= 0 || rect.height <= 0) return false;
    for (let node = el; node && node.nodeType === 1; node = node.parentElement) {
      const style = getComputedStyle(node);
      const opacity = Number.parseFloat(style.opacity || "1");
      if (style.display === "none" || style.visibility === "hidden" || style.contentVisibility === "hidden" || (Number.isFinite(opacity) && opacity <= 0.01)) return false;
    }
    return true;
  }

  const isRoot = (el) => !el || el === document.body || el === document.documentElement;

  // An element with overflow auto/scroll is a scroller on purpose; what sits
  // inside it past the edge is reachable by scrolling it.
  function isIntentionalScroller(el, axis) {
    if (isRoot(el)) return false;
    const style = getComputedStyle(el);
    const value = axis === "x" ? style.overflowX : style.overflowY;
    return value === "auto" || value === "scroll";
  }

  function hasScrollerAncestor(el, axis) {
    for (let node = el; node && node.nodeType === 1 && !isRoot(node); node = node.parentElement) {
      if (isIntentionalScroller(node, axis)) return true;
    }
    return false;
  }

  function hasReachableVerticalScrollerAncestor(el) {
    for (let node = el && el.parentElement; node && !isRoot(node); node = node.parentElement) {
      if (isIntentionalScroller(node, "y")) {
        const rect = node.getBoundingClientRect();
        if (rect.bottom > 0 && rect.top < (window.innerHeight || 0)) return true;
      }
    }
    return false;
  }

  function rootVerticalScrollLocked() {
    return [document.documentElement, document.body].filter(Boolean).some((node) => {
      const value = getComputedStyle(node).overflowY;
      return value === "hidden" || value === "clip";
    });
  }

  function paddingBoxRect(el) {
    const rect = el.getBoundingClientRect();
    const style = getComputedStyle(el);
    return { left: rect.left + toPx(style.borderLeftWidth), right: rect.right - toPx(style.borderRightWidth), top: rect.top + toPx(style.borderTopWidth), bottom: rect.bottom - toPx(style.borderBottomWidth) };
  }

  function textFragments(el) {
    const descend = isSemanticTextBoundary(el);
    const nodes = [];
    const pending = [...((el && el.childNodes) || [])];
    while (pending.length > 0) {
      const node = pending.shift();
      if (!node) continue;
      if (node.nodeType === 3) {
        if (String(node.textContent || "").trim()) nodes.push(node);
      } else if (descend && node.nodeType === 1) {
        pending.unshift(...node.childNodes);
      }
    }
    const fragments = [];
    for (const textNode of nodes) {
      const range = document.createRange();
      range.selectNodeContents(textNode);
      fragments.push(...[...range.getClientRects()].filter((rect) => rect.width > 0 && rect.height > 0));
      if (range.detach) range.detach();
    }
    return fragments;
  }

  const isIntentionalTruncation = (style) => style.textOverflow === "ellipsis" || Number.parseInt(style.webkitLineClamp || "0", 10) > 0;

  function hasVisualMask(style) {
    const maskImage = String(style.maskImage || style.webkitMaskImage || "none").toLowerCase();
    const clipPath = String(style.clipPath || "none").toLowerCase();
    return (maskImage !== "none" && maskImage !== "") || (clipPath !== "none" && clipPath !== "");
  }

  // A rounded box with overflow hidden clips corners on purpose.
  function isRoundedOverflowMask(style) {
    const clips = style.overflowX === "hidden" || style.overflowX === "clip" || style.overflowY === "hidden" || style.overflowY === "clip";
    if (!clips) return false;
    return [style.borderTopLeftRadius, style.borderTopRightRadius, style.borderBottomRightRadius, style.borderBottomLeftRadius].some((value) => toPx(value) > 0);
  }

  function hasVisualMaskAncestor(el) {
    for (let node = el; node && node.nodeType === 1; node = node.parentElement) {
      const style = getComputedStyle(node);
      if (hasVisualMask(style) || isRoundedOverflowMask(style)) return true;
    }
    return false;
  }

  // The clipping ancestors of el (overflow hidden/clip), with their padding box.
  function clippingBoundariesFor(el) {
    const boundaries = [];
    for (let node = el && el.parentElement; node && !isRoot(node); node = node.parentElement) {
      const style = getComputedStyle(node);
      const axes = [];
      if (style.overflowX === "hidden" || style.overflowX === "clip") axes.push("horizontal");
      if (style.overflowY === "hidden" || style.overflowY === "clip") axes.push("vertical");
      if (axes.length > 0 && !hasVisualMask(style) && !isRoundedOverflowMask(style)) boundaries.push({ el: node, box: paddingBoxRect(node), axes });
    }
    return boundaries;
  }

  // The usual screen-reader-only recipe: a tiny clipped box.
  function isStandardVisuallyHidden(style, rect) {
    const positioned = style.position === "absolute" || style.position === "fixed";
    const clipped = style.overflowX === "hidden" || style.overflowX === "clip";
    const legacyClip = String(style.clip || "").toLowerCase();
    const clipPath = String(style.clipPath || "").toLowerCase();
    const hasClip = legacyClip !== "auto" || (clipPath !== "none" && clipPath !== "");
    return positioned && clipped && rect.width <= 2 && rect.height <= 2 && (style.whiteSpace === "nowrap" || hasClip);
  }

  function hasStandardVisuallyHiddenAncestor(el) {
    for (let node = el; node && node.nodeType === 1; node = node.parentElement) {
      if (isStandardVisuallyHidden(getComputedStyle(node), node.getBoundingClientRect())) return true;
    }
    return false;
  }

  // Diagrams (the whiteboard embed, SVG) lay themselves out; masked and
  // screen-reader-only content is deliberate.
  function isExcluded(el) {
    return !!(el.closest && el.closest(".mermaid,svg")) || isForumUi(el) || hasVisualMaskAncestor(el) || hasStandardVisuallyHiddenAncestor(el);
  }

  function collectElements() {
    return [...((document.body && document.body.querySelectorAll("*")) || [])].filter((el) => el instanceof Element && !isForumUi(el)).slice(0, MAX_ELEMENTS);
  }

  // Elements that are being animated move on purpose, so they are skipped.
  function animationTarget(animation) {
    const target = animation.effect && animation.effect.target;
    if (target instanceof Element) return target;
    return target && target.element instanceof Element ? target.element : null;
  }

  function activeAnimations() {
    if (typeof document.getAnimations !== "function") return [];
    return document.getAnimations().filter((a) => ["running", "pending"].includes(String(a.playState))).filter((a) => !isForumUi(animationTarget(a)));
  }

  const activeAnimationTargets = () => activeAnimations().map(animationTarget).filter(Boolean);
  const isAnimated = (el, targets) => targets.some((target) => target === el || target.contains(el) || el.contains(target));

  function pushFinding(findings, seen, finding) {
    if (findings.length >= MAX_FINDINGS) return;
    const axis = finding.axis === "vertical" ? "vertical" : "horizontal";
    const selector = finding.selector || "";
    const key = finding.kind + ":" + selector + ":" + axis;
    if (seen.has(key)) return;
    seen.add(key);
    findings.push({ selector, kind: String(finding.kind), axis, overflowPx: roundPx(finding.overflowPx), severity: "error" });
  }

  function auditSevereTextOverflow(el, findings, seen, animated, failedRoots) {
    if (isRoot(el) || isExcluded(el) || !auditedText(el)) return;
    if (!isSemanticTextBoundary(el) && hasSemanticTextBoundaryAncestor(el)) return;
    if (failedRoots.some((root) => root.contains(el)) || isAnimated(el, animated)) return;
    const rect = el.getBoundingClientRect();
    if (!isVisible(el, rect)) return;
    const style = getComputedStyle(el);
    const fragments = textFragments(el);
    let severe = classifySevereTextOverflow({ fragments, box: paddingBoxRect(el), overflowX: style.overflowX, overflowY: style.overflowY, isTruncated: isIntentionalTruncation(style) });
    let failureRoot = el;
    for (const boundary of clippingBoundariesFor(el)) {
      const ancestor = classifySevereTextOverflow({
        fragments,
        box: boundary.box,
        overflowX: boundary.axes.includes("horizontal") ? "hidden" : "auto",
        overflowY: boundary.axes.includes("vertical") ? "hidden" : "auto",
        isTruncated: isIntentionalTruncation(style),
      });
      if (ancestor && (!severe || ancestor.overflowPx > severe.overflowPx)) {
        severe = ancestor;
        failureRoot = boundary.el;
      }
    }
    if (!severe) return;
    failedRoots.push(failureRoot);
    pushFinding(findings, seen, { selector: selectorOf(failureRoot), kind: severe.kind, axis: severe.axis, overflowPx: severe.overflowPx });
  }

  function escapesViewport(rect, viewportWidth, minOutsidePx) {
    return classifyMaterialRectEscape({ rect, boundary: { left: 0, right: viewportWidth, top: 0, bottom: window.innerHeight || 0 }, axes: ["horizontal"], minOutsidePx });
  }

  // Does meaningful content (text, a control) really sit past the right edge?
  function hasMaterialViewportEscape(el, viewportWidth, animated) {
    if (hasScrollerAncestor(el, "x") || isAnimated(el, animated) || isExcluded(el)) return false;
    if (!isSemanticTextBoundary(el) && hasSemanticTextBoundaryAncestor(el)) return false;
    const rect = el.getBoundingClientRect();
    if (!isVisible(el, rect)) return false;
    const style = getComputedStyle(el);
    const positioned = style.position === "absolute" || style.position === "fixed" || style.position === "sticky";
    if (positioned && !isRequiredControl(el)) return false;
    if (isRequiredControl(el)) {
      const escape = escapesViewport(rect, viewportWidth, 4);
      return !!escape && escape.side === "end";
    }
    if (!auditedText(el)) return false;
    const materialPx = Math.max(24, viewportWidth * 0.05);
    return textFragments(el).some((fragment) => {
      const escape = escapesViewport(fragment, viewportWidth, materialPx);
      return !!escape && escape.side === "end";
    });
  }

  // Does the element paint something the user sees (a fill, a border, an image)?
  // A wide box with text only at its left is still content past the edge.
  function paintsBox(el, style) {
    if (el.matches && el.matches("img,video,canvas,table,iframe")) return true;
    if (!backgroundIsTransparent(style.backgroundColor) || (style.backgroundImage && style.backgroundImage !== "none")) return true;
    return ["Top", "Right", "Bottom", "Left"].some((side) => toPx(style["border" + side + "Width"]) > 0 && style["border" + side + "Style"] !== "none");
  }

  function backgroundIsTransparent(color) {
    const value = String(color || "").trim().toLowerCase();
    if (!value || value === "transparent") return true;
    const rgba = value.match(/^rgba\(([^)]+)\)$/);
    if (!rgba) return false;
    const parts = rgba[1].split(/[\s,/]+/).filter(Boolean);
    return parts.length >= 4 && Number(parts[3]) === 0;
  }

  // A visible, painted, in-flow box that runs past the right edge of the
  // viewport by a material amount, with nothing clipping or scrolling it:
  // that is what makes the page scroll sideways. Positioned boxes are skipped
  // (off-canvas menus and the like sit out there on purpose).
  function hasEscapedPaintedBox(el, viewportWidth, animated) {
    if (isRoot(el) || hasScrollerAncestor(el, "x") || isAnimated(el, animated) || isExcluded(el)) return false;
    const rect = el.getBoundingClientRect();
    if (!isVisible(el, rect)) return false;
    const style = getComputedStyle(el);
    if (["absolute", "fixed", "sticky"].includes(style.position) || !paintsBox(el, style)) return false;
    if (clippingBoundariesFor(el).some((boundary) => boundary.axes.includes("horizontal"))) return false;
    const escape = escapesViewport(rect, viewportWidth, Math.max(24, viewportWidth * 0.05));
    return !!escape && escape.side === "end";
  }

  // The root scrolls sideways unless html or body clips (or hides) overflow-x.
  function rootHorizontalScrollLocked() {
    return [document.documentElement, document.body].filter(Boolean).some((node) => {
      const value = getComputedStyle(node).overflowX;
      return value === "hidden" || value === "clip";
    });
  }

  // Text left of the viewport can never be scrolled to.
  function auditUnreachableLeftText(el, viewportWidth, findings, seen, animated) {
    if (hasScrollerAncestor(el, "x") || isAnimated(el, animated) || isExcluded(el)) return;
    if (!isSemanticTextBoundary(el) && hasSemanticTextBoundaryAncestor(el)) return;
    if (!auditedText(el)) return;
    const rect = el.getBoundingClientRect();
    if (!isVisible(el, rect)) return;
    const style = getComputedStyle(el);
    if (["absolute", "fixed", "sticky"].includes(style.position) && !isRequiredControl(el)) return;
    const materialPx = Math.max(24, viewportWidth * 0.05);
    let escape = null;
    for (const fragment of textFragments(el)) {
      const candidate = escapesViewport(fragment, viewportWidth, materialPx);
      if (candidate && candidate.side === "start" && (!escape || candidate.overflowPx > escape.overflowPx)) escape = candidate;
    }
    if (escape) pushFinding(findings, seen, { selector: selectorOf(el), kind: "viewport-unreachable-content", axis: "horizontal", overflowPx: escape.overflowPx });
  }

  function auditRequiredControlBounds(el, viewportWidth, findings, seen, animated, failedRoots) {
    if (!isRequiredControl(el) || isExcluded(el) || isAnimated(el, animated)) return;
    const rect = el.getBoundingClientRect();
    if (!isVisible(el, rect)) return;

    let clipped = null;
    for (const boundary of clippingBoundariesFor(el)) {
      const escape = classifyMaterialRectEscape({ rect, boundary: boundary.box, axes: boundary.axes });
      if (escape && (!clipped || escape.overflowPx > clipped.escape.overflowPx)) clipped = { boundary, escape };
    }
    if (clipped && !failedRoots.some((root) => root === clipped.boundary.el || root.contains(clipped.boundary.el))) {
      failedRoots.push(clipped.boundary.el);
      pushFinding(findings, seen, { selector: selectorOf(clipped.boundary.el), kind: "clipped-control", axis: clipped.escape.axis, overflowPx: clipped.escape.overflowPx });
    }

    const horizontal = hasScrollerAncestor(el, "x") ? null : escapesViewport(rect, viewportWidth, 4);
    if (horizontal && horizontal.side === "start") {
      pushFinding(findings, seen, { selector: selectorOf(el), kind: "viewport-unreachable-control", axis: "horizontal", overflowPx: horizontal.overflowPx });
    }

    const style = getComputedStyle(el);
    const fixedToViewport = style.position === "fixed" || style.position === "sticky";
    const lockedToViewport = rootVerticalScrollLocked() && !hasReachableVerticalScrollerAncestor(el);
    const scrollY = Number(window.scrollY || window.pageYOffset || 0);
    const pinned = fixedToViewport || lockedToViewport;
    const vertical = classifyMaterialRectEscape({
      rect: pinned ? rect : { top: rect.top + scrollY, bottom: rect.bottom + scrollY, height: rect.height },
      boundary: pinned ? { top: 0, bottom: window.innerHeight || 0 } : { top: 0, bottom: document.documentElement.scrollHeight },
      axes: ["vertical"],
    });
    if (vertical) pushFinding(findings, seen, { selector: selectorOf(el), kind: "viewport-unreachable-control", axis: "vertical", overflowPx: vertical.overflowPx });
  }

  function backgroundIsOpaque(el) {
    const style = getComputedStyle(el);
    if (Number.parseFloat(style.opacity || "1") < 0.95) return false;
    const color = String(style.backgroundColor || "").trim().toLowerCase();
    if (!color || color === "transparent") return false;
    const rgba = color.match(/^rgba?\(([^)]+)\)$/);
    if (!rgba) return false;
    const parts = rgba[1].split(/[\s,/]+/).filter(Boolean);
    if (parts.length < 4) return true;
    const alpha = Number(parts[3]);
    return Number.isFinite(alpha) && alpha >= 0.95;
  }

  function effectiveOpacityTo(node, stopParent) {
    let opacity = 1;
    for (let current = node; current && current !== stopParent; current = current.parentElement) {
      const value = Number.parseFloat(getComputedStyle(current).opacity || "1");
      if (Number.isFinite(value)) opacity *= value;
    }
    return opacity;
  }

  // The opaque sibling subtree painted over point, if any.
  function opaqueSiblingBlocker(el, point, animated) {
    const top = document.elementFromPoint(point.x, point.y);
    if (!(top instanceof Element) || top === el || el.contains(top) || top.contains(el) || isForumUi(top)) return null;
    const targetAncestors = [];
    for (let node = el; node && !isRoot(node); node = node.parentElement) targetAncestors.push(node);
    let foundOpaqueSurface = false;
    for (let node = top; node && !isRoot(node); node = node.parentElement) {
      if (isAnimated(node, animated)) return null;
      if (backgroundIsOpaque(node)) foundOpaqueSurface = true;
      const siblingOf = targetAncestors.find((target) => target.parentElement === node.parentElement);
      if (siblingOf && foundOpaqueSurface && effectiveOpacityTo(top, node.parentElement) >= 0.95) return node;
    }
    return null;
  }

  function samplePoints(fragment) {
    const ratios = [0.2, 0.5, 0.8];
    return ratios.flatMap((xr) => ratios.map((yr) => ({ x: fragment.left + fragment.width * xr, y: fragment.top + fragment.height * yr })));
  }

  function auditTextOcclusion(elements, viewportWidth, findings, seen, animated) {
    const candidates = elements
      .filter((el) => !isExcluded(el))
      .filter((el) => {
        const text = auditedText(el);
        return text.length >= 8 || (text.length > 0 && isRequiredControl(el));
      })
      .filter((el) => isSemanticTextBoundary(el) || !hasSemanticTextBoundaryAncestor(el))
      .filter((el) => isVisible(el))
      .filter((el) => getComputedStyle(el).position !== "fixed")
      .filter((el) => !isAnimated(el, animated))
      .slice(0, 200);
    const failedRoots = [];
    for (const el of candidates) {
      if (failedRoots.some((root) => root.contains(el))) continue;
      const blockers = new Map();
      let totalSamples = 0;
      for (const fragment of textFragments(el)) {
        if (rectArea(fragment) < 16) continue;
        for (const point of samplePoints(fragment)) {
          if (point.x < 0 || point.y < 0 || point.x > viewportWidth || point.y > window.innerHeight) continue;
          totalSamples += 1;
          const blocker = opaqueSiblingBlocker(el, point, animated);
          if (blocker) blockers.set(blocker, (blockers.get(blocker) || 0) + 1);
        }
      }
      const occludedSamples = Math.max(0, ...blockers.values());
      if (!isNearTotalOcclusion({ occludedSamples, totalSamples })) continue;
      failedRoots.push(el);
      pushFinding(findings, seen, { selector: selectorOf(el), kind: "overlapping-text", axis: "horizontal", overflowPx: 0 });
    }
  }

  function auditLayout() {
    const viewportWidth = window.innerWidth || document.documentElement.clientWidth || 0;
    const findings = [];
    const seen = new Set();
    const elements = collectElements();
    const animated = activeAnimationTargets();
    const pageOverflowPx = document.documentElement.scrollWidth - viewportWidth;
    const escapedContent =
      !rootHorizontalScrollLocked() && elements.some((el) => hasMaterialViewportEscape(el, viewportWidth, animated) || hasEscapedPaintedBox(el, viewportWidth, animated));
    if (isMaterialPageOverflow({ overflowPx: pageOverflowPx, viewportWidth, hasEscapedContent: escapedContent })) {
      pushFinding(findings, seen, { selector: "html", kind: "page-horizontal-overflow", axis: "horizontal", overflowPx: pageOverflowPx });
    }
    const failedClippingRoots = [];
    for (const el of elements) auditRequiredControlBounds(el, viewportWidth, findings, seen, animated, failedClippingRoots);
    for (const el of elements) auditUnreachableLeftText(el, viewportWidth, findings, seen, animated);
    for (const el of elements) auditSevereTextOverflow(el, findings, seen, animated, failedClippingRoots);
    auditTextOcclusion(elements, viewportWidth, findings, seen, animated);
    return findings;
  }

  // ------------------------------------------------------------------ settling

  // Waits for count frames. A hidden tab never paints, so each wait also has a
  // timer fallback: the audit then runs late instead of never.
  const nextFrames = (count) =>
    new Promise((resolve) => {
      const step = (remaining) => {
        if (remaining <= 0) return resolve();
        let done = false;
        const next = () => {
          if (done) return;
          done = true;
          step(remaining - 1);
        };
        if (window.requestAnimationFrame) window.requestAnimationFrame(next);
        window.setTimeout(next, 250);
      };
      step(count);
    });

  function fontsReady() {
    try {
      if (document.fonts && document.fonts.ready) return document.fonts.ready.catch(() => {});
    } catch {
      /* the ResizeObserver settle below is still a safety net */
    }
    return Promise.resolve();
  }

  // quiet resolves once observe(onChange) has seen nothing change for SETTLE_MS
  // (or MAX_WAIT_MS has passed); value is what it resolves to when quiet.
  function quiet(observe, value) {
    return new Promise((resolve) => {
      let settleTimer = 0;
      let maxTimer = 0;
      let done = false;
      let stop = () => {};
      const finish = (result) => {
        if (done) return;
        done = true;
        window.clearTimeout(settleTimer);
        window.clearTimeout(maxTimer);
        stop();
        resolve(result);
      };
      const arm = () => {
        window.clearTimeout(settleTimer);
        settleTimer = window.setTimeout(() => finish(value), SETTLE_MS);
      };
      stop = observe(arm) || stop;
      arm();
      maxTimer = window.setTimeout(() => finish(!value), MAX_WAIT_MS);
    });
  }

  const resizeSettle = () =>
    quiet((arm) => {
      if (typeof ResizeObserver === "undefined") return null;
      const observer = new ResizeObserver(arm);
      for (const el of [document.documentElement, document.body, ...(document.body ? document.body.querySelectorAll("*") : [])].filter(Boolean).slice(0, MAX_ELEMENTS)) observer.observe(el);
      return () => observer.disconnect();
    }, true);

  // Resolves true when the DOM stopped mutating (hydration finished).
  const domQuiescent = () =>
    typeof MutationObserver === "undefined"
      ? Promise.resolve(false)
      : quiet((arm) => {
          const observer = new MutationObserver(arm);
          observer.observe(document.documentElement, { attributes: true, characterData: true, childList: true, subtree: true });
          return () => observer.disconnect();
        }, true);

  async function finiteAnimationsSettle() {
    const finite = activeAnimations().filter((a) => {
      const timing = a.effect && a.effect.getComputedTiming && a.effect.getComputedTiming();
      return timing && Number.isFinite(Number(timing.endTime));
    });
    // Infinite animations keep running; the audit only skips their targets.
    if (finite.length === 0) return true;
    let settled = false;
    await Promise.race([
      Promise.all(finite.map((a) => a.finished.catch(() => {}))).then(() => {
        settled = true;
      }),
      new Promise((resolve) => window.setTimeout(resolve, ANIMATION_MAX_WAIT_MS)),
    ]);
    if (!settled) for (const a of finite) a.finished.then(schedule, schedule);
    return settled;
  }

  // A pass reports its own completeness. An incomplete pass is uncertainty,
  // never evidence that an earlier finding is gone.
  function publish(findings, complete, targetPresenceComplete) {
    const viewportWidth = window.innerWidth || document.documentElement.clientWidth || 0;
    const severe = findings.filter((f) => f && f.severity === "error");
    const signature = JSON.stringify({ complete, targetPresenceComplete, viewportWidth, severe });
    if (signature === lastSignature) return;
    lastSignature = signature;
    window.parent.postMessage(
      {
        type: "forum:layout",
        artifact_version: DOC_VERSION,
        complete,
        target_presence_complete: targetPresenceComplete === true,
        viewport_width: viewportWidth,
        findings: severe.map((f) => ({ kind: f.kind, selector: f.selector, axis: f.axis, overflow_px: f.overflowPx })),
      },
      "*",
    );
  }

  async function runAudit(runId) {
    await fontsReady();
    await resizeSettle();
    const animationsSettled = await finiteAnimationsSettle();
    await nextFrames(2);
    if (runId !== auditRun) return;
    const first = auditLayout();
    await new Promise((resolve) => window.setTimeout(resolve, STABLE_SAMPLE_MS));
    await nextFrames(2);
    if (runId !== auditRun) return;
    const second = auditLayout();
    const hydrated = await domQuiescent();
    if (runId !== auditRun) return;
    const final = hydrated ? auditLayout() : second;
    const targetPresenceComplete = document.readyState === "complete" && hydrated;
    publish(findStableLayoutFindings(hydrated ? second : first, final), animationsSettled && targetPresenceComplete, targetPresenceComplete);
  }

  function schedule() {
    if (auditTimer) window.clearTimeout(auditTimer);
    const runId = ++auditRun;
    auditTimer = window.setTimeout(() => {
      runAudit(runId).catch(() => {
        if (runId === auditRun) publish([], false, false);
      });
    }, 50);
  }

  // Only what can change layout re-audits: the first settle, the load event and
  // a finished resize. animationend/transitionend are not listened to - they
  // bubble from every hover and fade in the document, and finite animations
  // already reschedule the audit themselves when they settle.
  let resizeTimer = 0;
  schedule();
  window.addEventListener("load", schedule, { once: true });
  window.addEventListener(
    "resize",
    () => {
      window.clearTimeout(resizeTimer);
      resizeTimer = window.setTimeout(schedule, RESIZE_DEBOUNCE_MS);
    },
    { passive: true },
  );

  window.forumLayout = Object.freeze(api);
})();
