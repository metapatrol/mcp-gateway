import {
  afterRenderEffect,
  ChangeDetectionStrategy,
  Component,
  computed,
  DestroyRef,
  ElementRef,
  inject,
  signal,
  viewChild,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { MpLoadState, MpPageHeader, MpPill } from '@metapatrol/ng';
import { Condition } from '../../api/models';
import { CONDITIONS, firstProblem, healthy, SERVER_CONDITIONS, tone } from '../../core/conditions';
import { buildGraph, condense, focusOn, GraphNode, Kind, routeOf } from '../../core/graph';
import { Lists } from '../../core/lists';

interface Issue {
  kind: string;
  name: string;
  route: string[];
  condition: Condition;
  tone: 'bad' | 'warn';
}

interface Tile {
  label: string;
  path: string;
  total: number;
  ready: number;
}

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

@Component({
  selector: 'app-overview',
  imports: [MpPageHeader, RouterLink, MpPill, MpLoadState],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './overview.html',
})
export class Overview {
  protected readonly lists = inject(Lists);
  protected readonly columns = COLUMNS;
  protected readonly routeOf = routeOf;

  protected readonly tiles = computed<Tile[]>(() => {
    const l = this.lists;
    const count = <T extends { status: { conditions: Condition[] } }>(
      items: T[],
      types: Condition['type'][],
    ) => ({
      total: items.length,
      ready: items.filter((i) => healthy(i.status.conditions, types)).length,
    });
    return [
      { label: 'Servers', path: '/servers', ...count(l.serverItems(), SERVER_CONDITIONS) },
      { label: 'Tool sets', path: '/toolsets', ...count(l.toolSetItems(), CONDITIONS) },
      { label: 'Capabilities', path: '/capabilities', ...count(l.capabilityItems(), CONDITIONS) },
      { label: 'Grants', path: '/grants', ...count(l.grantItems(), CONDITIONS) },
    ];
  });

  protected readonly issues = computed<Issue[]>(() => {
    const out: Issue[] = [];
    const add = (
      kind: string,
      base: string,
      name: string,
      conditions: Condition[],
      types: Condition['type'][],
    ) => {
      const c = firstProblem(conditions, types);
      if (c) {
        out.push({
          kind,
          name,
          route: ['/', base, name],
          condition: c,
          tone: tone(c) === 'warn' ? 'warn' : 'bad',
        });
      }
    };
    const l = this.lists;
    l.serverItems().forEach((s) =>
      add('server', 'servers', s.meta.name, s.status.conditions, SERVER_CONDITIONS),
    );
    l.toolSetItems().forEach((t) =>
      add('tool set', 'toolsets', t.meta.name, t.status.conditions, CONDITIONS),
    );
    l.capabilityItems().forEach((c) =>
      add('capability', 'capabilities', c.meta.name, c.status.conditions, CONDITIONS),
    );
    l.grantItems().forEach((g) =>
      add('grant', 'grants', g.meta.name, g.status.conditions, CONDITIONS),
    );
    // The resources at fault first, the ones only passing it on after.
    return out.sort((a, b) => Number(a.tone === 'warn') - Number(b.tone === 'warn'));
  });

  /** The map as drawn: long columns keep what has a problem and bundle the rest. */
  protected readonly graph = computed(() => {
    const l = this.lists;
    return condense(
      buildGraph(l.serverItems(), l.toolSetItems(), l.capabilityItems(), l.grantItems()),
    );
  });

  protected readonly hovered = signal<string | null>(null);
  protected readonly lit = computed(() => {
    const h = this.hovered();
    return h ? focusOn(this.graph().edges, [h]) : null;
  });

  private readonly map = viewChild<ElementRef<HTMLElement>>('map');
  protected readonly lines = signal<Edge[]>([]);
  protected readonly box = signal({ width: 0, height: 0 });

  constructor() {
    // The lines connect boxes wherever the layout put them, so they are drawn
    // after it: once per graph, and again whenever the map changes size — the
    // window, the navigation folding in or out, a font arriving late.
    afterRenderEffect(() => {
      this.graph();
      this.layout();
    });
    let observer: ResizeObserver | undefined;
    afterRenderEffect(() => {
      const el = this.map()?.nativeElement;
      observer?.disconnect();
      if (el && typeof ResizeObserver !== 'undefined') {
        observer = new ResizeObserver(() => this.layout());
        observer.observe(el);
      }
    });
    inject(DestroyRef).onDestroy(() => observer?.disconnect());
  }

  protected isLit(id: string): boolean {
    const l = this.lit();
    return !l || l.has(id);
  }

  protected edgeLit(e: Edge): boolean {
    const l = this.lit();
    return !!l && l.has(e.from) && l.has(e.to);
  }

  protected nodeTitle(n: GraphNode): string {
    return n.missing ? `${n.name}: not found` : n.name;
  }

  protected bundleLabel(n: GraphNode, title: string): string {
    return `${n.bundle} more ${n.bundle === 1 ? title.toLowerCase().replace(/s$/, '') : title.toLowerCase()}`;
  }

  private layout(): void {
    const el = this.map()?.nativeElement;
    if (!el) {
      return;
    }
    const origin = el.getBoundingClientRect();
    const pos = new Map<string, { left: number; right: number; y: number }>();
    el.querySelectorAll<HTMLElement>('[data-node]').forEach((n) => {
      const r = n.getBoundingClientRect();
      pos.set(n.dataset['node'] as string, {
        left: r.left - origin.left,
        right: r.right - origin.left,
        y: r.top - origin.top + r.height / 2,
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
    this.box.set({ width: origin.width, height: origin.height });
    this.lines.set(lines);
  }
}
