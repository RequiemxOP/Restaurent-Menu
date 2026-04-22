/**
 * pretextLayout.js
 * ──────────────────────────────────────────────────────────────
 * Reusable utility that wraps @chenglou/pretext for use inside
 * the vanilla-HTML Grilli restaurant site.
 *
 * Key design decisions
 * ─────────────────────
 * • prepare()  is called once per unique (text + font) pair and
 *   its result is cached so resize events only pay layout(), not
 *   the heavier measurement pass.
 * • layoutWithLines() is used so callers get per-line text strings
 *   they can render into absolutely-positioned DOM spans or canvas.
 * • All exports are named ES-module exports; consumers import{}
 *   from this file (also an ES module).
 */

import {
  prepareWithSegments,
  layoutWithLines,
  layout,
  measureLineStats,
  clearCache,
} from '../../../node_modules/@chenglou/pretext/dist/layout.js';

// ─── internal prepare cache ──────────────────────────────────────
/** @type {Map<string, import('../../../node_modules/@chenglou/pretext/dist/layout.js').PreparedTextWithSegments>} */
const _prepareCache = new Map();

/**
 * Build a stable cache key for a (text, font, options) triple.
 * @param {string} text
 * @param {string} font
 * @param {object} [options]
 * @returns {string}
 */
function _cacheKey(text, font, options = {}) {
  return `${font}|${JSON.stringify(options)}|${text}`;
}

// ─── Public API ──────────────────────────────────────────────────

/**
 * @typedef {Object} FontSettings
 * @property {string}  font          - Canvas font shorthand, e.g. "700 32px Forum"
 * @property {number}  lineHeight    - Line-height in px, e.g. 44
 * @property {'normal'|'pre-wrap'} [whiteSpace]
 * @property {'normal'|'keep-all'} [wordBreak]
 * @property {number}  [letterSpacing]
 */

/**
 * @typedef {Object} LineInfo
 * @property {string} text    - Text content of the line
 * @property {number} width   - Measured px width of the line
 * @property {number} index   - 0-based line index
 * @property {number} y       - Top y-offset in px (index * lineHeight)
 */

/**
 * @typedef {Object} LayoutResult
 * @property {LineInfo[]} lines
 * @property {number}     totalHeight  - Total block height in px
 * @property {number}     lineCount
 * @property {number}     maxWidth     - Width of the container used
 */

/**
 * computeLayout()
 * ──────────────────────────────────────────────────────────────
 * Main entry point.  Calls prepare() once (cached) and layout()
 * every call.  Safe to call on every ResizeObserver tick.
 *
 * @param {string}       text         - The text to lay out
 * @param {number}       maxWidth     - Container width in px
 * @param {FontSettings} fontSettings
 * @returns {LayoutResult}
 */
export function computeLayout(text, maxWidth, fontSettings) {
  const {
    font,
    lineHeight,
    whiteSpace = 'normal',
    wordBreak = 'normal',
    letterSpacing,
  } = fontSettings;

  const options = { whiteSpace, wordBreak };
  if (letterSpacing !== undefined) options.letterSpacing = letterSpacing;

  // Retrieve or build the prepared handle
  const key = _cacheKey(text, font, options);
  let prepared = _prepareCache.get(key);
  if (!prepared) {
    prepared = prepareWithSegments(text, font, options);
    _prepareCache.set(key, prepared);
  }

  // layoutWithLines is the hot path — pure arithmetic over cached widths
  const { height, lineCount, lines: rawLines } = layoutWithLines(
    prepared,
    maxWidth,
    lineHeight
  );

  /** @type {LineInfo[]} */
  const lines = rawLines.map((l, i) => ({
    text: l.text,
    width: l.width,
    index: i,
    y: i * lineHeight,
  }));

  return { lines, totalHeight: height, lineCount, maxWidth };
}

/**
 * computeHeight()
 * ──────────────────────────────────────────────────────────────
 * Lightweight variant — returns only height + lineCount without
 * building line-text strings.  Useful for container measurements.
 *
 * @param {string}       text
 * @param {number}       maxWidth
 * @param {FontSettings} fontSettings
 * @returns {{ totalHeight: number, lineCount: number }}
 */
export function computeHeight(text, maxWidth, fontSettings) {
  const { font, lineHeight, whiteSpace = 'normal', wordBreak = 'normal', letterSpacing } = fontSettings;
  const options = { whiteSpace, wordBreak };
  if (letterSpacing !== undefined) options.letterSpacing = letterSpacing;

  const key = _cacheKey(text, font, options);
  let prepared = _prepareCache.get(key);
  if (!prepared) {
    prepared = prepareWithSegments(text, font, options);
    _prepareCache.set(key, prepared);
  }

  const { height, lineCount } = layout(prepared, maxWidth, lineHeight);
  return { totalHeight: height, lineCount };
}

/**
 * getLineStats()
 * ──────────────────────────────────────────────────────────────
 * Returns lineCount + maxLineWidth without allocating text strings.
 *
 * @param {string}       text
 * @param {number}       maxWidth
 * @param {FontSettings} fontSettings
 * @returns {{ lineCount: number, maxLineWidth: number }}
 */
export function getLineStats(text, maxWidth, fontSettings) {
  const { font, whiteSpace = 'normal', wordBreak = 'normal', letterSpacing } = fontSettings;
  const options = { whiteSpace, wordBreak };
  if (letterSpacing !== undefined) options.letterSpacing = letterSpacing;

  const key = _cacheKey(text, font, options);
  let prepared = _prepareCache.get(key);
  if (!prepared) {
    prepared = prepareWithSegments(text, font, options);
    _prepareCache.set(key, prepared);
  }

  return measureLineStats(prepared, maxWidth);
}

/**
 * invalidateCache()
 * ──────────────────────────────────────────────────────────────
 * Clears the module-level prepare cache AND Pretext's internal
 * canvas-measurement cache.  Call when fonts are swapped at runtime.
 */
export function invalidateCache() {
  _prepareCache.clear();
  clearCache();
}
