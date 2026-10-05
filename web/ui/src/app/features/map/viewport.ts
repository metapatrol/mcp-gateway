/** Where the map's content sits in its viewport: shifted by x, y and scaled by k. */
export interface View {
  x: number;
  y: number;
  k: number;
}

export interface Box {
  left: number;
  top: number;
  width: number;
  height: number;
}

export const MIN_SCALE = 0.1;
/** Below this, boxes cannot be read: a view fitted smaller starts at the first box instead. */
export const READABLE_SCALE = 0.5;
export const MAX_SCALE = 2;

const clamp = (k: number) => Math.min(MAX_SCALE, Math.max(MIN_SCALE, k));

/** Zooms by factor around a point of the viewport, which stays where it is. */
export function zoomAt(view: View, px: number, py: number, factor: number): View {
  const k = clamp(view.k * factor);
  const f = k / view.k;
  return { k, x: px - (px - view.x) * f, y: py - (py - view.y) * f };
}

/**
 * The view that shows a box of the content whole and centred, never larger
 * than its natural size: a few boxes are not blown up to fill the screen.
 */
export function fit(box: Box, viewport: { width: number; height: number }, padding = 24): View {
  const k = clamp(
    Math.min(
      1,
      (viewport.width - 2 * padding) / Math.max(box.width, 1),
      (viewport.height - 2 * padding) / Math.max(box.height, 1),
    ),
  );
  return {
    k,
    x: (viewport.width - box.width * k) / 2 - box.left * k,
    y: Math.max(padding, (viewport.height - box.height * k) / 2) - box.top * k,
  };
}

/**
 * The view that starts reading at the top: as wide as the viewport allows,
 * never larger than its natural size. A map is mostly taller than wide.
 */
export function fitWidth(width: number, viewport: { width: number }, padding = 24): View {
  const k = clamp(Math.min(1, (viewport.width - 2 * padding) / Math.max(width, 1)));
  return { k, x: (viewport.width - width * k) / 2, y: padding };
}

/**
 * The view that shows boxes of the content: all of them, when they fit
 * readably, and otherwise the first of them at the top, as wide as fits.
 */
export function show(
  boxes: Box[],
  contentWidth: number,
  viewport: { width: number; height: number },
  padding = 24,
): View | undefined {
  const all = around(boxes);
  if (!all) {
    return undefined;
  }
  const whole = fit(all, viewport, padding);
  if (whole.k >= READABLE_SCALE) {
    return whole;
  }
  const v = fitWidth(contentWidth, viewport, padding);
  return { ...v, y: padding - all.top * v.k };
}

/** The smallest box around all of the given boxes. */
export function around(boxes: Box[]): Box | undefined {
  if (!boxes.length) {
    return undefined;
  }
  const left = Math.min(...boxes.map((b) => b.left));
  const top = Math.min(...boxes.map((b) => b.top));
  const right = Math.max(...boxes.map((b) => b.left + b.width));
  const bottom = Math.max(...boxes.map((b) => b.top + b.height));
  return { left, top, width: right - left, height: bottom - top };
}
