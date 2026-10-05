import {
  around,
  fit,
  fitWidth,
  MAX_SCALE,
  MIN_SCALE,
  READABLE_SCALE,
  show,
  zoomAt,
} from './viewport';

describe('zoomAt', () => {
  it('keeps the point under the pointer where it is', () => {
    const view = { x: 40, y: -20, k: 1 };
    const next = zoomAt(view, 300, 200, 2);
    // The content point under (300, 200) before and after.
    const before = { x: (300 - view.x) / view.k, y: (200 - view.y) / view.k };
    const after = { x: (300 - next.x) / next.k, y: (200 - next.y) / next.k };
    expect(after).toEqual(before);
    expect(next.k).toBe(2);
  });

  it('stays within its scale limits', () => {
    expect(zoomAt({ x: 0, y: 0, k: 1 }, 0, 0, 100).k).toBe(MAX_SCALE);
    expect(zoomAt({ x: 0, y: 0, k: 1 }, 0, 0, 0.001).k).toBe(MIN_SCALE);
  });
});

describe('fit', () => {
  it('shrinks a large map to the viewport, centred', () => {
    const v = fit({ left: 0, top: 0, width: 2000, height: 1000 }, { width: 1048, height: 800 }, 24);
    expect(v.k).toBe(0.5);
    expect(v.x).toBe(24);
  });

  it('does not blow a small part up beyond its natural size', () => {
    expect(
      fit({ left: 500, top: 300, width: 200, height: 100 }, { width: 1000, height: 800 }).k,
    ).toBe(1);
  });
});

describe('around', () => {
  it('encloses every box, and nothing for none', () => {
    expect(
      around([
        { left: 10, top: 20, width: 5, height: 5 },
        { left: 0, top: 40, width: 30, height: 10 },
      ]),
    ).toEqual({ left: 0, top: 20, width: 30, height: 30 });
    expect(around([])).toBeUndefined();
  });
});

describe('fitWidth', () => {
  it('fits the width and starts at the top, however tall the map is', () => {
    expect(fitWidth(1120, { width: 608 }, 24)).toEqual({ k: 0.5, x: 24, y: 24 });
    expect(fitWidth(500, { width: 1000 }).k).toBe(1);
  });
});

describe('show', () => {
  const vp = { width: 1000, height: 800 };

  it('fits boxes that fit readably', () => {
    expect(show([{ left: 0, top: 100, width: 200, height: 40 }], 1120, vp)?.k).toBe(1);
  });

  it('starts at the first of boxes spread too far to read at once', () => {
    const v = show(
      [
        { left: 0, top: 1000, width: 200, height: 40 },
        { left: 0, top: 9000, width: 200, height: 40 },
      ],
      1120,
      vp,
      24,
    );
    expect(v?.k).toBeGreaterThanOrEqual(READABLE_SCALE);
    // The first box sits at the top of the viewport.
    expect((v?.y ?? 0) + 1000 * (v?.k ?? 0)).toBeCloseTo(24);
  });
});
