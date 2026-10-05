import {
  afterRenderEffect,
  ChangeDetectionStrategy,
  Component,
  computed,
  DestroyRef,
  effect,
  ElementRef,
  inject,
  input,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { MpListSearch, MpLoadState, MpPageHeader } from '@metapatrol/ng';
import { buildGraph, focusOn, GraphNode, Kind, matching, routeOf } from '../../core/graph';
import { Lists } from '../../core/lists';
import { Box, fit, fitWidth, show, View, zoomAt } from './viewport';

interface Edge {
  d: string;
  from: string;
  to: string;
  broken: boolean;
}

const COLUMNS: { kind: Kind; title: string }[] = [
  { kind: 'grant', title: 'Grants' },
  { kind: 'capability', title: 'Capabilities' },
  { kind: 'toolset', title: 'Tool sets' },
  { kind: 'server', title: 'Servers' },
];

/** A drag shorter than this is a click. */
const DRAG_PX = 4;

@Component({
  selector: 'app-map',
  imports: [MpPageHeader, RouterLink, MpLoadState, MpListSearch],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './map.html',
})
export class MapPage {
  protected readonly lists = inject(Lists);
  protected readonly columns = COLUMNS;
  protected readonly routeOf = routeOf;

  /** ?focus=: the chains through every box whose name, or a grant's subject, contains it. */
  readonly focus = input<string>();

  protected readonly graph = computed(() => {
    const l = this.lists;
    return buildGraph(l.serverItems(), l.toolSetItems(), l.capabilityItems(), l.grantItems());
  });
  protected readonly boxes = computed(() =>
    Object.values(this.graph().columns).reduce((n, c) => n + c.length, 0),
  );
  protected readonly matches = computed(() => {
    const q = this.focus()?.trim();
    return q ? new Set(matching(this.graph(), q)) : null;
  });
  private readonly focused = computed(() => {
    const m = this.matches();
    return m ? focusOn(this.graph().edges, m) : null;
  });

  protected readonly hovered = signal<string | null>(null);
  /** What is lit: the chain under the pointer, else the chains ?focus= names. */
  protected readonly lit = computed(() => {
    const h = this.hovered();
    return h ? focusOn(this.graph().edges, [h]) : this.focused();
  });

  private readonly viewport = viewChild<ElementRef<HTMLElement>>('viewport');
  private readonly content = viewChild<ElementRef<HTMLElement>>('content');
  private readonly zoomLabel = viewChild<ElementRef<HTMLElement>>('zoomLabel');
  protected readonly lines = signal<Edge[]>([]);
  protected readonly size = signal({ width: 0, height: 0 });

  /**
   * Zoom and pan. Deliberately no signal: they change with every pointer
   * event, and are written straight to the content's transform once per
   * frame, so neither the template nor the lines are looked at again.
   */
  private view: View = { x: 0, y: 0, k: 1 };
  private frame = 0;

  protected drag?: { x: number; y: number; view: View; moved: boolean };
  private suppressClick = false;
  private fitted = false;

  constructor() {
    // Lines are drawn in the content's own coordinates, so zooming and
    // panning move them with the boxes and never redraw them.
    afterRenderEffect(() => {
      this.graph();
      this.layout();
    });
    let observer: ResizeObserver | undefined;
    afterRenderEffect(() => {
      const el = this.content()?.nativeElement;
      observer?.disconnect();
      if (el && typeof ResizeObserver !== 'undefined') {
        observer = new ResizeObserver(() => this.layout());
        observer.observe(el);
      }
    });
    inject(DestroyRef).onDestroy(() => {
      observer?.disconnect();
      cancelAnimationFrame(this.frame);
    });

    // A new ?focus= zooms to the boxes it names — not their whole chains,
    // which may run from the top of one column to the bottom of another — or
    // back to the start. Only that: data arriving later leaves the reader's
    // zoom alone.
    effect(() => {
      const matches = this.matches();
      untracked(() => {
        if (this.fitted) {
          queueMicrotask(() => this.start(matches));
        }
      });
    });

    // Wheel and pointer are listened to directly, not through the template:
    // a template listener has Angular check the whole map after each event,
    // and these come once per frame and change nothing it renders.
    afterRenderEffect((onCleanup) => {
      const vp = this.viewport()?.nativeElement;
      if (!vp) {
        return;
      }
      // A drag that ends on a box is no click on it: swallowed before the
      // box's link sees it.
      const swallow = (e: MouseEvent) => {
        if (this.suppressClick) {
          e.preventDefault();
          e.stopPropagation();
        }
      };
      const listeners: [string, (e: never) => void, AddEventListenerOptions][] = [
        ['wheel', (e: WheelEvent) => this.wheel(e), { passive: false }],
        ['pointerdown', (e: PointerEvent) => this.pointerDown(e), {}],
        ['pointermove', (e: PointerEvent) => this.pointerMove(e), {}],
        ['pointerup', () => this.pointerUp(), {}],
        ['pointercancel', () => this.pointerUp(), {}],
        ['click', swallow, { capture: true }],
      ];
      for (const [type, fn, opts] of listeners) {
        vp.addEventListener(type, fn as EventListener, opts);
      }
      onCleanup(() => {
        for (const [type, fn, opts] of listeners) {
          vp.removeEventListener(type, fn as EventListener, opts);
        }
      });
    });
  }

  protected isLit(id: string): boolean {
    const l = this.lit();
    return !l || l.has(id);
  }

  protected edgeLit(e: Edge): boolean {
    const l = this.lit();
    return !!l && l.has(e.from) && l.has(e.to);
  }

  /** Lights a chain under the pointer, but not while the map is dragged across boxes. */
  protected hover(id: string | null): void {
    if (!this.drag?.moved) {
      this.hovered.set(id);
    }
  }

  protected nodeTitle(n: GraphNode): string {
    return n.missing ? `${n.name}: not found` : n.name;
  }

  // ---------------------------------------------------------------- zoom and pan

  protected zoom(factor: number): void {
    const vp = this.viewport()?.nativeElement;
    if (vp) {
      this.setView(zoomAt(this.view, vp.clientWidth / 2, vp.clientHeight / 2, factor));
    }
  }

  protected fitAll(): void {
    this.fitTo(null);
  }

  /** Where the map opens: at the matches of ?focus=, or at its top, as wide as fits. */
  private start(matches: Set<string> | null): void {
    const vp = this.viewport()?.nativeElement;
    const content = this.content()?.nativeElement;
    if (!vp || !content) {
      return;
    }
    if (matches?.size) {
      this.fitTo(matches);
    } else {
      this.setView(fitWidth(content.offsetWidth, { width: vp.clientWidth }));
    }
    this.fitted = true;
  }

  private wheel(event: WheelEvent): void {
    event.preventDefault();
    const r = (event.currentTarget as HTMLElement).getBoundingClientRect();
    this.setView(
      zoomAt(
        this.view,
        event.clientX - r.left,
        event.clientY - r.top,
        Math.exp(-event.deltaY * 0.0015),
      ),
    );
  }

  private pointerDown(event: PointerEvent): void {
    if (event.button !== 0) {
      return;
    }
    this.drag = { x: event.clientX, y: event.clientY, view: this.view, moved: false };
  }

  private pointerMove(event: PointerEvent): void {
    const d = this.drag;
    if (!d) {
      return;
    }
    const dx = event.clientX - d.x;
    const dy = event.clientY - d.y;
    if (!d.moved && Math.hypot(dx, dy) < DRAG_PX) {
      return;
    }
    if (!d.moved) {
      d.moved = true;
      this.hovered.set(null);
      (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
    }
    this.setView({ ...d.view, x: d.view.x + dx, y: d.view.y + dy });
  }

  private pointerUp(): void {
    if (this.drag?.moved) {
      this.suppressClick = true;
      setTimeout(() => (this.suppressClick = false));
    }
    this.drag = undefined;
  }

  private setView(view: View): void {
    this.view = view;
    if (this.frame) {
      return;
    }
    this.frame = requestAnimationFrame(() => {
      this.frame = 0;
      const v = this.view;
      const content = this.content()?.nativeElement;
      if (content) {
        content.style.transform = `translate(${v.x}px, ${v.y}px) scale(${v.k})`;
      }
      const label = this.zoomLabel()?.nativeElement;
      if (label) {
        label.textContent = `${Math.round(v.k * 100)}%`;
      }
    });
  }

  /** Shows the boxes of ids whole, or the whole map without ids. */
  private fitTo(ids: Set<string> | null): void {
    const vp = this.viewport()?.nativeElement;
    const content = this.content()?.nativeElement;
    if (!vp || !content) {
      return;
    }
    const boxes: Box[] = [];
    content.querySelectorAll<HTMLElement>('[data-node]').forEach((n) => {
      if (!ids || ids.has(n.dataset['node'] as string)) {
        boxes.push(this.boxOf(n, content));
      }
    });
    const viewport = { width: vp.clientWidth, height: vp.clientHeight };
    const view = ids
      ? show(boxes, content.offsetWidth, viewport)
      : fit(
          { left: 0, top: 0, width: content.offsetWidth, height: content.offsetHeight },
          viewport,
        );
    if (view) {
      this.setView(view);
    }
  }

  /** A box in the content's own coordinates, whatever the zoom. */
  private boxOf(el: HTMLElement, content: HTMLElement): Box {
    const origin = content.getBoundingClientRect();
    const r = el.getBoundingClientRect();
    const k = this.view.k;
    return {
      left: (r.left - origin.left) / k,
      top: (r.top - origin.top) / k,
      width: r.width / k,
      height: r.height / k,
    };
  }

  private layout(): void {
    const content = this.content()?.nativeElement;
    if (!content) {
      return;
    }
    const pos = new Map<string, { left: number; right: number; y: number }>();
    content.querySelectorAll<HTMLElement>('[data-node]').forEach((n) => {
      const b = this.boxOf(n, content);
      pos.set(n.dataset['node'] as string, {
        left: b.left,
        right: b.left + b.width,
        y: b.top + b.height / 2,
      });
    });
    const lines: Edge[] = [];
    for (const e of this.graph().edges) {
      const a = pos.get(e.from);
      const b = pos.get(e.to);
      if (!a || !b) {
        continue;
      }
      const mid = (a.right + b.left) / 2;
      lines.push({ ...e, d: `M${a.right},${a.y} C${mid},${a.y} ${mid},${b.y} ${b.left},${b.y}` });
    }
    this.size.set({ width: content.offsetWidth, height: content.offsetHeight });
    this.lines.set(lines);
    if (!this.fitted && lines.length) {
      this.start(this.matches());
    }
  }
}
