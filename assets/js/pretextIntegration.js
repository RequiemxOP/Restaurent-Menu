/**
 * pretextIntegration.js
 * ────────────────────────────────────────────────────────────────
 * Integrates @chenglou/pretext into the Grilli restaurant site.
 *
 * What this module does
 * ─────────────────────
 * 1. Picks up two DOM targets already in index.html:
 *    • .testi-text  – the testimonial quote paragraph
 *    • .hero-title  – every hero-slider h1 (one visible at a time)
 *
 * 2. For each target it:
 *    a. Hides the original text node but keeps the element so CSS
 *       classes / transitions are undisturbed.
 *    b. Computes layout via pretextLayout.computeLayout() (Pretext).
 *    c. Renders each line as an absolutely-positioned <span> inside
 *       a relative-positioned container injected into the element.
 *    d. Optionally draws debug line-boxes (toggled by
 *       ?pretext-debug=1 in the URL or the floating toggle button).
 *
 * 3. Re-runs layout on every ResizeObserver tick.
 *    • Only layout() is re-called (cheap); prepare() is cached.
 *
 * Before vs. After
 * ─────────────────
 *  BEFORE: Browser's own inline text flow handles wrapping.
 *  AFTER:  Pretext measures, wraps, and positions every line
 *          explicitly, giving us pixel-accurate control and a
 *          visible debug overlay.
 */

import { computeLayout, invalidateCache } from './utils/pretextLayout.js';

// ─── Helpers ─────────────────────────────────────────────────────

/** Resolve the computed CSS font shorthand for an element. */
function resolveFont(el) {
  const s = window.getComputedStyle(el);
  // Canvas font string: "style variant weight size/lineHeight family"
  // We build a minimal, valid shorthand Pretext can parse.
  const weight = s.fontWeight;
  const size = s.fontSize;                    // e.g. "48px"
  const family = s.fontFamily;               // e.g. '"Forum", serif'
  return `${weight} ${size} ${family}`;
}

/** Read line-height in px from computed style. */
function resolveLineHeight(el) {
  const s = window.getComputedStyle(el);
  const raw = s.lineHeight;
  if (raw === 'normal') {
    // Fallback: 1.4× font-size
    return parseFloat(s.fontSize) * 1.4;
  }
  return parseFloat(raw);
}

/** Extract the plain-text content of an element (strips inner HTML). */
function extractText(el) {
  return el.innerText.trim();
}

// ─── Debug overlay ───────────────────────────────────────────────

const DEBUG_OVERLAY_STYLE = `
  position: absolute;
  left: 0;
  height: 100%;
  top: 0;
  pointer-events: none;
  border: 1px solid rgba(255, 100, 0, 0.6);
  background: rgba(255, 140, 0, 0.07);
  box-sizing: border-box;
`;

/** Draw/refresh per-line debug boxes inside a wrapper. */
function renderDebugBoxes(wrapper, lines, lineHeight) {
  // Remove old boxes
  wrapper.querySelectorAll('.pretext-debug-box').forEach(b => b.remove());

  lines.forEach(line => {
    const box = document.createElement('div');
    box.className = 'pretext-debug-box';
    box.style.cssText = DEBUG_OVERLAY_STYLE;
    box.style.top = `${line.y}px`;
    box.style.width = `${line.width}px`;
    box.style.height = `${lineHeight}px`;
    // Label
    box.title = `Line ${line.index + 1}: "${line.text}" — ${Math.round(line.width)}px`;
    wrapper.appendChild(box);
  });
}

// ─── Core renderer ───────────────────────────────────────────────

/**
 * A single managed text target.
 */
class PretextTarget {
  /**
   * @param {HTMLElement} el         – the original element (e.g. <p> or <h1>)
   * @param {{ debug: boolean }}     options
   */
  constructor(el, options) {
    this.el = el;
    this.options = options;
    this.wrapper = null;   // the relative-positioned line container
    this._init();
  }

  _init() {
    const el = this.el;

    // Grab text & computed font BEFORE hiding anything
    this.originalText = extractText(el);
    this.font = resolveFont(el);
    this.lineHeight = resolveLineHeight(el);

    // Hide original text content by making it transparent
    // (we don't remove the node so screen-readers still see it)
    el.style.color = 'transparent';
    el.style.position = 'relative';
    el.style.overflow = 'visible';

    // Build a sibling overlay wrapper inside the same element
    const wrapper = document.createElement('span');
    wrapper.className = 'pretext-wrapper';
    wrapper.setAttribute('aria-hidden', 'true');
    wrapper.style.cssText = `
      position: absolute;
      top: 0; left: 0; right: 0;
      pointer-events: none;
      color: inherit;
      font: inherit;
      line-height: inherit;
      white-space: nowrap;
    `;
    el.appendChild(wrapper);
    this.wrapper = wrapper;

    // Initial render
    this._render();

    // Responsive: re-layout on container resize
    this._ro = new ResizeObserver(() => this._render());
    this._ro.observe(el);
  }

  _render() {
    const { el, wrapper, originalText, font, lineHeight } = this;
    const maxWidth = el.clientWidth || el.offsetWidth || 600;
    if (maxWidth <= 0) return;

    // ── Pretext layout ────────────────────────────────────────
    const { lines, totalHeight } = computeLayout(
      originalText,
      maxWidth,
      { font, lineHeight }
    );

    // Set wrapper height so the parent el expands correctly
    wrapper.style.height = `${totalHeight}px`;
    el.style.height = `${totalHeight}px`;

    // Clear old line spans
    wrapper.querySelectorAll('.pretext-line').forEach(s => s.remove());

    // Render each line as an absolutely-positioned span
    lines.forEach(line => {
      const span = document.createElement('span');
      span.className = 'pretext-line';
      span.textContent = line.text;
      span.style.cssText = `
        display: block;
        position: absolute;
        left: 0;
        top: ${line.y}px;
        white-space: nowrap;
        color: inherit;
        font: inherit;
      `;
      wrapper.appendChild(span);
    });

    // ── Debug overlay ─────────────────────────────────────────
    if (this.options.debug) {
      renderDebugBoxes(wrapper, lines, lineHeight);
    }
  }

  /** Toggle debug boxes without re-running layout. */
  setDebug(enabled) {
    this.options.debug = enabled;
    if (!enabled) {
      this.wrapper.querySelectorAll('.pretext-debug-box').forEach(b => b.remove());
    } else {
      this._render();
    }
  }

  destroy() {
    this._ro.disconnect();
    this.wrapper.remove();
    this.el.style.color = '';
    this.el.style.position = '';
    this.el.style.overflow = '';
    this.el.style.height = '';
  }
}

// ─── Floating toggle button ───────────────────────────────────────

function buildDebugToggle(targets) {
  const btn = document.createElement('button');
  btn.id = 'pretext-debug-toggle';
  btn.textContent = '⬛ Pretext Debug: OFF';
  btn.style.cssText = `
    position: fixed;
    bottom: 24px;
    left: 24px;
    z-index: 9999;
    padding: 10px 18px;
    background: #1a1a2e;
    color: #e2c08d;
    border: 1.5px solid #e2c08d;
    border-radius: 8px;
    font: 700 13px/1 'DM Sans', sans-serif;
    cursor: pointer;
    box-shadow: 0 4px 20px rgba(0,0,0,0.5);
    transition: background 0.2s, color 0.2s;
    letter-spacing: 0.5px;
  `;

  let debugOn = false;

  btn.addEventListener('click', () => {
    debugOn = !debugOn;
    targets.forEach(t => t.setDebug(debugOn));
    btn.textContent = debugOn ? '🟠 Pretext Debug: ON' : '⬛ Pretext Debug: OFF';
    btn.style.background = debugOn ? '#e2c08d' : '#1a1a2e';
    btn.style.color = debugOn ? '#1a1a2e' : '#e2c08d';
  });

  document.body.appendChild(btn);
}

// ─── Banner ──────────────────────────────────────────────────────

function buildBanner() {
  const banner = document.createElement('div');
  banner.id = 'pretext-banner';
  banner.innerHTML = `
    <span style="font-weight:700">⚡ Pretext</span>
    text layout engine active
    <span id="pretext-count" style="
      background:rgba(255,255,255,0.15);
      padding:1px 7px;
      border-radius:99px;
      margin-left:6px;
      font-size:11px;
      font-weight:700;
    ">0 targets</span>
  `;
  banner.style.cssText = `
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    z-index: 10000;
    background: linear-gradient(90deg, #c8954a 0%, #e2c08d 100%);
    color: #1a1a2e;
    font: 600 12px/1.5 'DM Sans', sans-serif;
    text-align: center;
    padding: 5px 16px;
    letter-spacing: 0.4px;
    pointer-events: none;
    box-shadow: 0 2px 12px rgba(0,0,0,0.25);
  `;
  document.body.prepend(banner);
  return banner;
}

// ─── Entry point ─────────────────────────────────────────────────

function init() {
  // Wait for fonts to be ready so resolveFont() gets real values
  document.fonts.ready.then(() => {
    // Flush any stale cache from previous font loads
    invalidateCache();

    const debugParam = new URLSearchParams(window.location.search).get('pretext-debug');
    const debugDefault = debugParam === '1';

    /** @type {PretextTarget[]} */
    const targets = [];

    // ── Target 1: Testimonial quote (.testi-text) ─────────────
    const testiEl = document.querySelector('.testi-text');
    if (testiEl) {
      targets.push(new PretextTarget(testiEl, { debug: debugDefault }));
    }

    // ── Target 2: Hero slide headings (.hero-title) ───────────
    document.querySelectorAll('.hero-title').forEach(el => {
      targets.push(new PretextTarget(el, { debug: debugDefault }));
    });

    // ── Target 3: Section subhead demonstration (.section-text) ──
    //    First .section-text on the page (Service section)
    const sectionTexts = document.querySelectorAll('.section-text');
    if (sectionTexts.length > 0) {
      targets.push(new PretextTarget(sectionTexts[0], { debug: debugDefault }));
    }

    if (targets.length === 0) return;

    // Update banner count
    const banner = buildBanner();
    const countBadge = banner.querySelector('#pretext-count');
    if (countBadge) countBadge.textContent = `${targets.length} target${targets.length > 1 ? 's' : ''}`;

    // Floating debug toggle
    buildDebugToggle(targets);

    console.log(
      `%c[Pretext] %c${targets.length} targets managed. Toggle debug with the button (bottom-left) or add ?pretext-debug=1 to the URL.`,
      'color:#c8954a;font-weight:700',
      'color:inherit'
    );
  });
}

// Run after DOM is ready
if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', init);
} else {
  init();
}
