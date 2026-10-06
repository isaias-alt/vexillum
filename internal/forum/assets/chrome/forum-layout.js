// Passive layout audit. It runs inside the artifact document, looks for layout
// failures that are provable from rendered geometry, and posts what it finds
// to the chrome around the iframe as a "forum:layout" message. It never
// writes to the artifact (no style, class, attribute, scroll or focus change),
// never listens to hover or animation events, and talks to nobody but
// window.parent.
//
// Principle: report only when geometry proves a viewer loses something they
// need, and stay silent on everything that looks deliberate. When a signal is
// ambiguous the audit says nothing; a missed finding costs the user nothing,
// a false one costs their trust in the tray.
//
// Layout of this file:
//   1. thresholds (each one derived from fixtures, see the comments)
//   2. pure geometry: judge() turns a plain-data snapshot into findings
//   3. the sampler: readSnapshot() turns a live document into that snapshot
//   4. selectors for the elements findings point at
//   5. the scheduler: when to audit, how a finding is confirmed, what is sent
(function () {
  "use strict";

  const framed = window.parent !== window;

  // ------------------------------------------------------------ 1. thresholds
  //
  // Method for every number below: name the symptom, render benign and failing
  // fixtures in headless Chrome at device pixel ratios 1, 1.25 and 2 (macOS
  // Chrome, Latin text, 12 to 40 px), record the largest benign reading and the
  // smallest failing one that matters, then put the threshold at least 2x above
  // the first and below the second. The fixtures live in
  // testdata/layout_calibration.html (it prints the raw readings); the
  // real-Chrome test in layout_chrome_test.go re-checks both sides of the
  // boundaries that matter.
  const THRESHOLD = Object.freeze({
    // Symptom: a reader loses part of a character at the end of a line.
    // Benign: text in a box sized to it, sub-pixel widths, italic, letter-spacing
    // (its trailing gap is subtracted first) read at most 0.73 px at every ratio.
    // Failing: the narrowest glyph (an "i" at 12 px) is 2.7 px. 2 px is 2.7x
    // the benign reading and under one glyph.
    hiddenTextPx: 2,
    // Symptom: a line is no longer readable because its lower or upper part is
    // cut. Share of one line box. Benign: line-height 1 clips 0.09 of the line
    // (descender tails), and display headings at line-height 0.8 clip 0.21.
    // Failing: a box half a line tall clips 0.46 to 0.5. 0.44 is 2.1x the
    // benign reading and under the smallest failing one.
    hiddenTextLines: 0.44,
    // Symptom: a control cannot be operated in full. Benign: a button in a box
    // sized to it reads 0.7 px (0.006 of its width) at every ratio. Failing: the
    // label starts to lose glyphs once the cut passes the padding, 13 px
    // (0.12 of a 110 px button). Floor and share are both required, so a
    // 20 px icon button needs 4 px and a wide one needs 12 percent.
    controlCutPx: 4,
    controlCutMin: 0.12,
    // Above this share the control is hidden rather than cut (a carousel slide,
    // a collapsed panel, an off-canvas drawer): a judgement call, not a reading.
    controlCutMax: 0.6,
    // Symptom: a horizontal scrollbar for a reason other than rounding. Benign:
    // sub-pixel overshoot reads 1 px; an element as wide as 100vw next to a
    // classic 17 px scrollbar reads 17 px (the largest gutter in common use).
    // Failing: 48 px of real overshoot is a page that visibly slides sideways.
    // 40 px is 2.4x the gutter and under the failing case.
    wideByPx: 40,
    // Symptom: a reader cannot reach what they came to read. Benign: hanging
    // punctuation pulled out of the page by design reads 4 px (2 percent of the
    // line). Failing: most of the text is out (60 px of a 200 px line is a
    // lost first word, the whole line is 400 px). 8 px is 2x the benign
    // pull; half the text is the line where a word is certainly gone.
    beyondReachPx: 8,
    beyondReachShare: 0.5,
    // Symptom: a reader cannot read a word because something opaque sits on it.
    // The text is sampled on an 8-column grid per line, so shares move in steps
    // of 1/8. Benign: a badge over a quarter of a line reads 0.25. Failing: a
    // cover over 70 percent of a line reads 0.75. 0.6 is 2.4x the benign
    // reading and means most of the text is gone. Fewer than 6 testable points
    // (the rest are off screen; the audit never scrolls) prove nothing.
    buriedShare: 0.6,
    buriedMinPoints: 6,
    // Text or controls parked this far beyond an edge are hidden on purpose
    // (the usual off-screen idioms use 9999 px); real mistakes in the fixtures
    // reached 400 px. 1000 px is 2.5x the largest mistake and 10x under the idiom.
    farAwayPx: 1000,
    // A container whose box is thinner than this is collapsed, not clipping
    // (visually hidden text sits in a 1 px box, a collapsing panel passes
    // through 0 to 2 px; no box a person reads text in is that thin).
    collapsedPx: 3,
    // Painted opacity at or below this is invisible; at or above `opaque` a
    // background counts as covering.
    seeThrough: 0.02,
    opaque: 0.95,
  });

  // Budgets that keep one pass cheap and one report small.
  const BUDGET = Object.freeze({
    textOwners: 3000,
    controls: 800,
    buriedOwners: 400,
    buriedColumns: 8,
    buriedLines: 3,
    findings: 60,
    selector: 240,
  });

  // Timing. QUIET: the document must go this long without a mutation before it
  // counts as settled. SETTLE_CAP: after this long the audit stops waiting and
  // reports an incomplete pass. PAUSE: gap between the two confirming
  // observations, after two painted frames; longer than a frame or a typical
  // 150-300 ms transition tick would be needed to flip a transient state, and
  // far below the point where a person would wonder what is going on.
  const TIMING = Object.freeze({ quietMs: 400, settleCapMs: 8000, pauseMs: 300, resizeDebounceMs: 400, frameFallbackMs: 100 });

  const KIND_ORDER = Object.freeze(["wide-page", "clipped-text", "cut-off-control", "unreachable-control", "unreachable-text", "buried-text"]);

  // ------------------------------------------------------------- 2. geometry
  //
  // A snapshot is plain data, in client (viewport) coordinates:
  //   page:  { sideways, overflowX, reach: {l,t,r,b} }  (reach = where scrolling can take a viewer)
  //   items: [{ role: "text"|"control", ref, rect:{l,t,r,b}, lineH, slack,
  //             clips:[{ ref, l,t,r,b, x, y, softX, softY }], scrollX, scrollY,
  //             pinned, cover: {share, px, counted} | null }]
  // Sides: a positive overflow_px is past the right or bottom edge, a negative
  // one past the left or top edge.

  const width = (r) => r.r - r.l;
  const height = (r) => r.b - r.t;
  const round1 = (n) => Math.round(n * 10) / 10;

  // The part of an item's box that survives every container that clips it.
  function visibleBox(item) {
    let box = { l: item.rect.l, t: item.rect.t, r: item.rect.r, b: item.rect.b };
    for (const c of item.clips) {
      if (c.x && !item.scrollX) {
        box.l = Math.max(box.l, c.l);
        box.r = Math.min(box.r, c.r);
      }
      if (c.y && !item.scrollY) {
        box.t = Math.max(box.t, c.t);
        box.b = Math.min(box.b, c.b);
      }
    }
    return box;
  }

  // How far the item crosses its clipping containers, per side, with the
  // container responsible for the largest crossing.
  function crossings(item) {
    const out = { right: { px: 0, ref: null }, left: { px: 0, ref: null }, bottom: { px: 0, ref: null }, top: { px: 0, ref: null } };
    const note = (side, px, ref) => {
      if (px > out[side].px) out[side] = { px, ref };
    };
    for (const c of item.clips) {
      if (c.x && !item.scrollX && !c.softX) {
        note("right", item.rect.r - c.r, c.ref);
        note("left", c.l - item.rect.l, c.ref);
      }
      if (c.y && !item.scrollY && !c.softY) {
        note("bottom", item.rect.b - c.b, c.ref);
        note("top", c.t - item.rect.t, c.ref);
      }
    }
    return out;
  }

  function judgeText(item, out) {
    const cut = crossings(item);
    const sides = [
      ["right", "horizontal", 1, Math.max(0, cut.right.px - item.slack), THRESHOLD.hiddenTextPx],
      ["left", "horizontal", -1, Math.max(0, cut.left.px - item.slack), THRESHOLD.hiddenTextPx],
      ["bottom", "vertical", 1, cut.bottom.px, THRESHOLD.hiddenTextLines * item.lineH],
      ["top", "vertical", -1, cut.top.px, THRESHOLD.hiddenTextLines * item.lineH],
    ];
    for (const [side, axis, sign, px, floor] of sides) {
      if (px < floor || px <= 0) continue;
      // Text pushed a very long way out of its box to the start side is the
      // image-replacement idiom (text-indent: -9999px): hidden on purpose.
      if (sign < 0 && px >= THRESHOLD.farAwayPx) continue;
      out.push({ kind: "clipped-text", ref: cut[side].ref, axis, overflow_px: sign * px });
    }
  }

  function judgeControlCut(item, out) {
    const cut = crossings(item);
    const w = width(item.rect);
    const h = height(item.rect);
    const sides = [
      ["right", "horizontal", 1, cut.right.px, w],
      ["left", "horizontal", -1, cut.left.px, w],
      ["bottom", "vertical", 1, cut.bottom.px, h],
      ["top", "vertical", -1, cut.top.px, h],
    ];
    for (const [, axis, sign, px, size] of sides) {
      if (px < THRESHOLD.controlCutPx || size <= 0) continue;
      const share = px / size;
      // Hidden past the upper share the control reads as tucked away on
      // purpose (collapsed panel, carousel slide, off-canvas drawer).
      if (share < THRESHOLD.controlCutMin || share > THRESHOLD.controlCutMax) continue;
      out.push({ kind: "cut-off-control", ref: item.ref, axis, overflow_px: sign * px });
    }
  }

  // Where scrolling cannot take anyone: the page's start sides, and its end
  // sides when the viewport does not scroll that way.
  function judgeReach(item, reach, out) {
    const kind = item.role === "control" ? "unreachable-control" : "unreachable-text";
    const axes = [
      ["horizontal", item.scrollX, item.rect.l, item.rect.r, reach.l, reach.r],
      ["vertical", item.scrollY, item.rect.t, item.rect.b, reach.t, reach.b],
    ];
    for (const [axis, scrolls, lo, hi, reachLo, reachHi] of axes) {
      if (scrolls) continue;
      const size = hi - lo;
      if (size <= 0) continue;
      const before = Math.max(0, Math.min(size, reachLo - lo));
      const after = Math.max(0, Math.min(size, hi - reachHi));
      const outside = Math.max(before, after);
      if (outside < THRESHOLD.beyondReachPx || outside / size < THRESHOLD.beyondReachShare) continue;
      // Parked far beyond the edge (the off-screen-for-assistive-tech idiom,
      // an off-canvas drawer) is hidden on purpose, whichever side it is on.
      const gap = before >= after ? reachLo - hi : lo - reachHi;
      if (gap >= THRESHOLD.farAwayPx) continue;
      out.push({ kind, ref: item.ref, axis, overflow_px: before >= after ? -outside : outside });
    }
  }

  function judge(snapshot) {
    const out = [];
    const page = snapshot.page;
    if (page.sideways && page.overflowX >= THRESHOLD.wideByPx) {
      out.push({ kind: "wide-page", ref: null, axis: "horizontal", overflow_px: page.overflowX });
    }
    for (const item of snapshot.items) {
      const box = visibleBox(item);
      // Nothing of it survives its containers: tucked away on purpose.
      if (width(box) <= 0 || height(box) <= 0) continue;
      if (item.role === "text") judgeText(item, out);
      else judgeControlCut(item, out);
      if (!item.pinned) judgeReach(item, page.reach, out);
      if (item.role === "text" && item.cover && item.cover.counted >= THRESHOLD.buriedMinPoints && item.cover.share >= THRESHOLD.buriedShare) {
        out.push({ kind: "buried-text", ref: item.ref, axis: "horizontal", overflow_px: item.cover.px });
      }
    }
    return merge(out);
  }

  // One finding per (kind, element, axis, side): the largest crossing wins.
  function merge(findings) {
    const best = new Map();
    for (const f of findings) {
      const key = f.kind + "|" + (f.ref === null ? "" : refKey(f.ref)) + "|" + f.axis + "|" + Math.sign(f.overflow_px);
      const prev = best.get(key);
      if (!prev || Math.abs(f.overflow_px) > Math.abs(prev.overflow_px)) best.set(key, f);
    }
    return [...best.values()];
  }

  const refIds = new WeakMap();
  let refCounter = 0;
  function refKey(ref) {
    if (typeof ref === "string") return "s:" + ref;
    if (!refIds.has(ref)) refIds.set(ref, ++refCounter);
    return "o:" + refIds.get(ref);
  }

  // Two observations agree on a finding when kind, selector and axis match; the
  // later observation supplies the magnitude (it is the settled one).
  function agree(first, second) {
    const key = (f) => f.kind + "\u0000" + f.selector + "\u0000" + f.axis;
    const seen = new Set(first.map(key));
    return second.filter((f) => seen.has(key(f)));
  }

  // Order and cap a finding list so the same document always yields the same
  // report. Returns the list and whether it had to be cut.
  function shape(findings) {
    const best = new Map();
    for (const f of findings) {
      const key = f.kind + "\u0000" + f.selector + "\u0000" + f.axis;
      const prev = best.get(key);
      if (!prev || Math.abs(f.overflow_px) > Math.abs(prev.overflow_px)) best.set(key, f);
    }
    const ordered = [...best.values()].sort((a, b) => {
      const k = KIND_ORDER.indexOf(a.kind) - KIND_ORDER.indexOf(b.kind);
      if (k !== 0) return k;
      const m = Math.abs(b.overflow_px) - Math.abs(a.overflow_px);
      if (m !== 0) return m;
      return a.selector < b.selector ? -1 : a.selector > b.selector ? 1 : a.axis < b.axis ? -1 : a.axis > b.axis ? 1 : 0;
    });
    return { findings: ordered.slice(0, BUDGET.findings), truncated: ordered.length > BUDGET.findings };
  }

  // ------------------------------------------------------------- 3. sampler
  const SKIPPED_TAGS = new Set(["SCRIPT", "STYLE", "NOSCRIPT", "TEMPLATE", "TITLE", "HEAD", "TEXTAREA", "SELECT", "OPTION", "SVG", "CANVAS", "IFRAME", "OBJECT"]);
  const CONTROL_SELECTOR = [
    "a[href]", "button", "input:not([type=\"hidden\"])", "select", "textarea", "summary",
    "[role=\"button\"]", "[role=\"link\"]", "[role=\"tab\"]", "[role=\"switch\"]", "[role=\"checkbox\"]", "[role=\"menuitem\"]", "[onclick]",
  ].join(",");
  const REPLACED_TAGS = new Set(["IMG", "VIDEO", "CANVAS", "PICTURE"]);

  const isForumUi = (el) => !!(el.closest && el.closest("[data-forum-ui]"));

  function alphaOf(color) {
    if (!color || color === "transparent") return 0;
    const slash = /\/\s*([\d.]+)(%?)\s*\)\s*$/.exec(color);
    if (slash) return Math.min(1, Number(slash[1]) / (slash[2] ? 100 : 1));
    const legacy = /^rgba\(\s*[^,]+,\s*[^,]+,\s*[^,]+,\s*([\d.]+)\s*\)$/.exec(color);
    return legacy ? Math.min(1, Number(legacy[1])) : 1;
  }

  // A style value that is present and not its "off" keyword.
  const isSet = (value, off) => !!value && value !== off;

  const isPureTranslation = (t) => {
    if (!t || t === "none") return true;
    const m = /^matrix\(([^)]*)\)$/.exec(t);
    if (!m) return false;
    const v = m[1].split(",").map(Number);
    return Math.abs(v[0] - 1) < 1e-3 && Math.abs(v[3] - 1) < 1e-3 && Math.abs(v[1]) < 1e-3 && Math.abs(v[2]) < 1e-3;
  };

  function readSnapshot(win, doc) {
    const html = doc.documentElement;
    const body = doc.body;
    const cache = new Map();
    const style = (el) => {
      if (!cache.has(el)) cache.set(el, win.getComputedStyle(el));
      return cache.get(el);
    };

    // Elements that something is currently moving: their geometry is a frame
    // of a motion, not a layout.
    const moving = new Set();
    for (const a of doc.getAnimations ? doc.getAnimations() : []) {
      const target = a.effect && a.effect.target;
      if (target && a.playState === "running") moving.add(target);
    }

    const se = doc.scrollingElement || html;
    const overflowX = style(html).overflowX !== "visible" || !body ? style(html).overflowX : style(body).overflowX;
    const overflowY = style(html).overflowY !== "visible" || !body ? style(html).overflowY : style(body).overflowY;
    const sideways = overflowX !== "hidden" && overflowX !== "clip";
    const downwards = overflowY !== "hidden" && overflowY !== "clip";
    const scrollX = win.scrollX || win.pageXOffset || 0;
    const scrollY = win.scrollY || win.pageYOffset || 0;
    const cw = se.clientWidth;
    const ch = se.clientHeight;
    const reachR = sideways ? Math.max(se.scrollWidth, cw) : cw;
    const reachB = downwards ? Math.max(se.scrollHeight, ch) : ch;
    const page = {
      sideways,
      overflowX: Math.max(0, se.scrollWidth - cw),
      reach: { l: -scrollX, t: -scrollY, r: reachR - scrollX, b: reachB - scrollY },
    };

    // What the ancestry of an element says about it, computed once per element.
    const ancestry = new Map();
    function about(el) {
      if (ancestry.has(el)) return ancestry.get(el);
      const parent = el.parentElement;
      const up = parent && parent !== html ? about(parent) : { opacity: 1, silent: false, pinned: false, translateOnly: true, hiddenFromTree: false };
      const cs = style(el);
      const own = {
        opacity: up.opacity * (Number(cs.opacity) || 0),
        silent:
          up.silent ||
          moving.has(el) ||
          isSet(cs.maskImage, "none") ||
          isSet(cs.webkitMaskImage, "none") ||
          isSet(cs.clipPath, "none") ||
          (cs.position === "absolute" && isSet(cs.clip, "auto")) ||
          cs.visibility !== "visible" ||
          cs.contentVisibility === "hidden" ||
          el.getAttribute("aria-hidden") === "true" ||
          el.hasAttribute("inert") ||
          !isPureTranslation(cs.transform),
        pinned: up.pinned || cs.position === "fixed" || cs.position === "sticky" || isSet(cs.transform, "none"),
      };
      ancestry.set(el, own);
      return own;
    }

    const makesBlockFor = (cs, position) => {
      if (position === "fixed") return cs.transform !== "none" || cs.filter !== "none" || cs.willChange === "transform";
      return cs.position !== "static" || cs.transform !== "none" || cs.filter !== "none" || cs.willChange === "transform";
    };

    // The containers whose overflow can cut el, nearest first. Overflow clips
    // what its box contains; a positioned element escapes ancestors that are
    // not its containing block, so the walk follows containing blocks.
    function containers(el, includeSelf) {
      const clips = [];
      let scrollableX = false;
      let scrollableY = false;
      let collapsed = false;
      let cur = el;
      let first = true;
      while (cur && cur !== html && cur !== body) {
        const position = style(cur).position;
        let block = null;
        if (position === "fixed" || position === "absolute") {
          for (let a = cur.parentElement; a && a !== html; a = a.parentElement) {
            if (makesBlockFor(style(a), position)) {
              block = a;
              break;
            }
          }
        } else {
          block = cur.parentElement;
        }
        const candidates = [];
        if (first && includeSelf && style(cur).display !== "inline" && style(cur).display !== "contents") candidates.push(cur);
        if (block && block !== html && block !== body) candidates.push(block);
        for (const a of candidates) {
          const cs = style(a);
          const clipX = cs.overflowX === "hidden" || cs.overflowX === "clip";
          const clipY = cs.overflowY === "hidden" || cs.overflowY === "clip";
          if (cs.overflowX === "auto" || cs.overflowX === "scroll") scrollableX = true;
          if (cs.overflowY === "auto" || cs.overflowY === "scroll") scrollableY = true;
          if (!clipX && !clipY) continue;
          const rect = a.getBoundingClientRect();
          const l = rect.left + a.clientLeft;
          const t = rect.top + a.clientTop;
          if (a.clientWidth < THRESHOLD.collapsedPx || a.clientHeight < THRESHOLD.collapsedPx) collapsed = true;
          const lineClamp = (cs.webkitLineClamp || cs.lineClamp || "none") !== "none";
          clips.push({
            ref: a, l, t, r: l + a.clientWidth, b: t + a.clientHeight, x: clipX, y: clipY,
            softX: clipX && cs.textOverflow === "ellipsis", softY: clipY && lineClamp,
          });
        }
        first = false;
        cur = block;
      }
      return { clips, scrollableX, scrollableY, collapsed };
    }

    const items = [];
    const root = body || html;

    // Text: one item per element that directly owns visible text.
    const owners = new Map();
    const walker = doc.createTreeWalker(root, 0x1 | 0x4, {
      acceptNode(node) {
        if (node.nodeType === 1) {
          if (SKIPPED_TAGS.has(node.tagName.toUpperCase()) || (node.hasAttribute && node.hasAttribute("data-forum-ui"))) return 2; // REJECT the subtree
          return 3; // SKIP, but visit children
        }
        return /\S/.test(node.nodeValue) ? 1 : 3;
      },
    });
    for (let node = walker.nextNode(); node && owners.size < BUDGET.textOwners; node = walker.nextNode()) {
      const owner = node.parentElement;
      if (!owner) continue;
      if (!owners.has(owner)) owners.set(owner, []);
      owners.get(owner).push(node);
    }
    let buriedChecked = 0;
    for (const [owner, nodes] of owners) {
      const info = about(owner);
      if (info.silent || info.opacity < THRESHOLD.seeThrough) continue;
      const lines = [];
      for (const node of nodes) {
        const range = doc.createRange();
        range.selectNodeContents(node);
        for (const r of range.getClientRects()) if (r.width > 0 && r.height > 0) lines.push({ l: r.left, t: r.top, r: r.right, b: r.bottom });
      }
      if (lines.length === 0) continue;
      const rect = { l: Infinity, t: Infinity, r: -Infinity, b: -Infinity };
      let lineH = Infinity;
      for (const r of lines) {
        rect.l = Math.min(rect.l, r.l);
        rect.t = Math.min(rect.t, r.t);
        rect.r = Math.max(rect.r, r.r);
        rect.b = Math.max(rect.b, r.b);
        lineH = Math.min(lineH, r.b - r.t);
      }
      const where = containers(owner, true);
      if (where.collapsed) continue;
      const cs = style(owner);
      const item = {
        role: "text", ref: owner, rect, lineH,
        slack: Math.max(0, parseFloat(cs.letterSpacing) || 0),
        clips: where.clips, scrollX: where.scrollableX, scrollY: where.scrollableY,
        pinned: info.pinned, cover: null,
      };
      const inView = rect.r > 0 && rect.l < win.innerWidth && rect.b > 0 && rect.t < win.innerHeight;
      if (inView && buriedChecked < BUDGET.buriedOwners) {
        buriedChecked += 1;
        item.cover = coverOf(owner, lines, rect, win, doc, style, about);
      }
      items.push(item);
    }

    // Controls.
    let controls = 0;
    for (const el of doc.querySelectorAll(CONTROL_SELECTOR)) {
      if (controls >= BUDGET.controls) break;
      if (isForumUi(el) || el.disabled) continue;
      const info = about(el);
      if (info.silent || info.opacity < THRESHOLD.seeThrough) continue;
      const r = el.getBoundingClientRect();
      if (r.width < 1 || r.height < 1) continue;
      controls += 1;
      const where = containers(el, false);
      if (where.collapsed) continue;
      items.push({
        role: "control", ref: el, rect: { l: r.left, t: r.top, r: r.right, b: r.bottom }, lineH: r.height, slack: 0,
        clips: where.clips, scrollX: where.scrollableX, scrollY: where.scrollableY, pinned: info.pinned, cover: null,
      });
    }
    return { page, items };
  }

  // How much of an element's text sits under something opaque. A grid of
  // points along its lines is hit-tested; points outside the viewport cannot be
  // tested (the audit never scrolls the page) and do not count either way.
  function coverOf(owner, lines, rect, win, doc, style, about) {
    const picked = [];
    const distinct = lines.slice().sort((a, b) => a.t - b.t || a.l - b.l);
    const rows = [];
    for (const l of distinct) if (rows.length === 0 || l.t - rows[rows.length - 1].t > 2) rows.push(l);
    const take = rows.length <= BUDGET.buriedLines ? rows : [rows[0], rows[Math.floor(rows.length / 2)], rows[rows.length - 1]];
    for (const l of take) {
      for (let i = 0; i < BUDGET.buriedColumns; i += 1) picked.push([l.l + ((i + 0.5) / BUDGET.buriedColumns) * (l.r - l.l), (l.t + l.b) / 2]);
    }
    let counted = 0;
    let covered = 0;
    for (const [x, y] of picked) {
      if (x < 0 || y < 0 || x >= win.innerWidth || y >= win.innerHeight) continue;
      counted += 1;
      const hit = doc.elementFromPoint(x, y);
      if (!hit || hit === owner || owner.contains(hit) || hit.contains(owner) || isForumUi(hit)) continue;
      const info = about(hit);
      if (info.pinned || info.silent || info.opacity < THRESHOLD.opaque) continue;
      const cs = style(hit);
      const paints =
        REPLACED_TAGS.has(hit.tagName.toUpperCase()) ||
        (alphaOf(cs.backgroundColor) >= THRESHOLD.opaque && cs.backgroundClip !== "text") ||
        /url\(/.test(cs.backgroundImage || "");
      if (paints && (cs.mixBlendMode || "normal") === "normal") covered += 1;
    }
    return { share: counted ? covered / counted : 0, px: counted ? (covered / counted) * (rect.r - rect.l) : 0, counted };
  }

  // ------------------------------------------------------------ 4. selectors
  //
  // A path a person or an agent can paste into querySelector: tag names, ids
  // that look hand-written, and :nth-of-type where siblings share a tag.
  // Generated ids (digits, hashes, framework colons) change per load, so they
  // are never used. Classes are left out for the same reason.
  const STABLE_ID = /^[A-Za-z_][A-Za-z0-9_-]{0,39}$/;
  const looksGenerated = (id) => /\d{3,}/.test(id) || /^[0-9a-f]{10,}$/i.test(id);

  function selectorFor(el, doc) {
    if (!el) return "";
    const parts = [];
    for (let node = el; node && node.nodeType === 1 && node !== doc.documentElement; node = node.parentElement) {
      const tag = node.tagName.toLowerCase();
      const id = node.getAttribute("id");
      if (id && STABLE_ID.test(id) && !looksGenerated(id) && doc.getElementById(id) === node) {
        parts.unshift(tag + "#" + id);
        break;
      }
      let segment = tag;
      const parent = node.parentElement;
      if (parent && node !== doc.body) {
        const same = [...parent.children].filter((s) => s.tagName === node.tagName);
        if (same.length > 1) segment += ":nth-of-type(" + (same.indexOf(node) + 1) + ")";
      }
      parts.unshift(segment);
    }
    while (parts.length > 1 && parts.join(" > ").length > BUDGET.selector) parts.shift();
    return parts.join(" > ").slice(0, BUDGET.selector);
  }

  // -------------------------------------------------------------- 5. runtime
  const script = document.currentScript;
  const artifactVersion = (() => {
    try {
      return (script && script.src && new URL(script.src, "http://x.invalid/").searchParams.get("av")) || "";
    } catch (e) {
      return "";
    }
  })();

  function start() {
    let generation = 0; // a new run bumps it; older runs notice and stop
    let lastSent = "";
    let mutated = false;
    let quietTimer = 0;
    let onQuietBreak = null;

    const observer = new MutationObserver((records) => {
      for (const r of records) {
        const t = r.target;
        if (r.type === "attributes" && r.attributeName && r.attributeName.indexOf("data-forum") === 0) continue;
        const el = t && t.nodeType === 1 ? t : t && t.parentElement;
        if (el && isForumUi(el)) continue;
        mutated = true;
        if (onQuietBreak) onQuietBreak();
        return;
      }
    });
    observer.observe(document, { subtree: true, childList: true, attributes: true, characterData: true });

    const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
    // Painted frames separate the two observations. A page nobody is looking at
    // (a background tab) paints none, so each wait also ends on its own after
    // FRAME_FALLBACK_MS: the pause still separates the samples, and the audit
    // is not held up until the tab comes to the front.
    const frames = async (n) => {
      for (let i = 0; i < n; i += 1) await Promise.race([new Promise((resolve) => requestAnimationFrame(resolve)), sleep(TIMING.frameFallbackMs)]);
    };

    // Resolves true when the document stayed quiet, false when the budget ran out.
    function untilQuiet(budgetMs) {
      return new Promise((resolve) => {
        const cap = setTimeout(() => finish(false), Math.max(0, budgetMs));
        const arm = () => {
          clearTimeout(quietTimer);
          quietTimer = setTimeout(() => finish(true), TIMING.quietMs);
        };
        const finish = (quiet) => {
          clearTimeout(cap);
          clearTimeout(quietTimer);
          onQuietBreak = null;
          resolve(quiet);
        };
        onQuietBreak = arm;
        arm();
      });
    }

    const finiteAnimations = () =>
      (document.getAnimations ? document.getAnimations() : []).filter((a) => {
        const timing = a.effect && a.effect.getComputedTiming ? a.effect.getComputedTiming() : null;
        return timing && Number.isFinite(timing.endTime) && a.playState === "running";
      });

    async function settle(started) {
      const left = () => TIMING.settleCapMs - (Date.now() - started);
      const capped = (promise) => Promise.race([promise, sleep(Math.max(0, left())).then(() => false)]);
      if (document.readyState !== "complete") {
        await capped(new Promise((resolve) => window.addEventListener("load", () => resolve(true), { once: true })));
      }
      if (document.fonts && document.fonts.ready) await capped(document.fonts.ready.then(() => true));
      const running = finiteAnimations();
      if (running.length) await capped(Promise.all(running.map((a) => a.finished.catch(() => {}))).then(() => true));
      return untilQuiet(left());
    }

    function observe() {
      const found = judge(readSnapshot(window, document));
      return found.map((f) => ({
        kind: f.kind,
        selector: f.ref === null ? "" : selectorFor(f.ref, document),
        axis: f.axis,
        overflow_px: round1(f.overflow_px),
      }));
    }

    function send(fields) {
      const payload = { artifact_version: artifactVersion, viewport_width: window.innerWidth, ...fields };
      const text = JSON.stringify(payload);
      if (text === lastSent) return;
      lastSent = text;
      window.parent.postMessage({ type: "forum:layout", ...payload }, "*");
    }

    async function run() {
      const mine = ++generation;
      const started = Date.now();
      try {
        const quiet = await settle(started);
        if (mine !== generation) return;
        mutated = false;
        const first = observe();
        await frames(2);
        await sleep(TIMING.pauseMs);
        if (mine !== generation) return;
        const second = observe();
        const confirmed = shape(agree(first, second));
        const stillMoving = finiteAnimations().length > 0;
        const loaded = document.readyState === "complete";
        const steady = quiet && loaded && !mutated;
        const fontsDone = !document.fonts || document.fonts.status !== "loading";
        send({
          complete: steady && fontsDone && !stillMoving && !confirmed.truncated,
          target_presence_complete: steady,
          findings: confirmed.findings,
        });
      } catch (error) {
        if (mine !== generation) return;
        send({ complete: false, target_presence_complete: false, findings: [] });
      }
    }

    let resizeTimer = 0;
    window.addEventListener("resize", () => {
      generation += 1; // whatever is in flight no longer describes this window
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(run, TIMING.resizeDebounceMs);
    });
    run();
  }

  if (framed) start();

  // Test access to the pure parts, never inside the chrome.
  if (!framed) {
    window.__forumLayoutKit = Object.freeze({
      judge, agree, shape, selectorFor, readSnapshot,
      thresholds: THRESHOLD, budget: BUDGET, timing: TIMING,
    });
  }
})();
