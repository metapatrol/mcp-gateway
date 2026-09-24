import { ChangeDetectionStrategy, Component, computed, inject, input, resource, signal } from '@angular/core';
import { getCapabilityMcp, getGrantMcp, getServerMcp, getSubjectMcp, getToolSetMcp } from '../api/functions';
import { AgentExchange, BackendExchange } from '../api/models';
import { Inspect, live } from '../core/inspect';
import { contextCost, heaviest, TOKENIZER, tokenCounter } from '../core/tokens';
import { JsonViewer } from './json-viewer';
import { LoadState } from './load-state';
import { Tip } from './tip';

export type McpOwner = 'server' | 'toolset' | 'capability' | 'grant' | 'subject';

interface Block {
  title: string;
  json: string;
  /** What of it reaches a model's context, once the tokenizer is loaded. */
  tokens?: number;
  tokensTip?: string;
}

const pretty = (v: unknown) => JSON.stringify(v, null, 2);

/**
 * What a resource comes to over MCP, as JSON: for a server, what the backend
 * answered the gateway; for the others, the gateway's own answers to
 * server/discover and tools/list for an agent holding the resource.
 */
@Component({
  selector: 'app-mcp-view',
  imports: [LoadState, JsonViewer, Tip],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <app-load-state [error]="doc.error()" [loading]="!doc.hasValue()" what="MCP view" />
    @if (cost(); as c) {
      <p class="mb-3 flex flex-wrap items-baseline gap-x-3 gap-y-1 text-[13px]">
        <span class="font-semibold tabular-nums" [appTip]="totalTip()" [focusable]="true">≈ {{ n(c.total) }} tokens in context</span>
        <span class="muted tabular-nums">tools {{ n(c.total - c.instructions) }}</span>
        <span class="muted tabular-nums">instructions {{ n(c.instructions) }}</span>
      </p>
    }
    <div class="flex flex-col gap-4">
      @for (b of blocks(); track b.title) {
        <section class="panel">
          <header class="panel-head">
            <h2 class="panel-title font-mono tracking-normal normal-case">{{ b.title }}</h2>
            <div class="flex items-center gap-3">
              @if (b.tokens !== undefined) {
                <span class="muted text-xs tabular-nums" [appTip]="b.tokensTip" [focusable]="!!b.tokensTip">≈ {{ n(b.tokens) }} tokens</span>
              }
              <button type="button" class="btn" (click)="copy(b)">{{ copied() === b.title ? 'Copied' : 'Copy' }}</button>
            </div>
          </header>
          <app-json-viewer [value]="b.json" />
        </section>
      }
    </div>
  `,
})
export class McpView {
  private readonly inspect = inject(Inspect);
  readonly kind = input.required<McpOwner>();
  readonly name = input.required<string>();

  protected readonly doc = live(
    () => ({ kind: this.kind(), name: this.name() }),
    ({ kind, name }): Promise<AgentExchange | BackendExchange> => {
      const api = this.inspect.api;
      switch (kind) {
        case 'server':
          return api.invoke(getServerMcp, { name });
        case 'toolset':
          return api.invoke(getToolSetMcp, { name });
        case 'capability':
          return api.invoke(getCapabilityMcp, { name });
        case 'grant':
          return api.invoke(getGrantMcp, { name });
        default:
          return api.invoke(getSubjectMcp, { subject: name });
      }
    },
  );

  private readonly counter = resource({ loader: () => tokenCounter() });

  protected readonly cost = computed(() => {
    const d = this.doc.hasValue() ? this.doc.value() : undefined;
    const count = this.counter.hasValue() ? this.counter.value() : undefined;
    return d && count ? contextCost(d, count) : undefined;
  });

  protected readonly totalTip = computed(() => {
    const c = this.cost();
    const tools = c?.pages.flatMap((p) => p.tools) ?? [];
    return c ? `${TOKENIZER} estimate · ${tools.length} tools\n\n${heaviest(tools)}` : undefined;
  });

  protected readonly n = (v: number) => v.toLocaleString('en');

  protected readonly blocks = computed<Block[]>(() => {
    const d = this.doc.hasValue() ? this.doc.value() : undefined;
    if (!d) {
      return [];
    }
    const cost = this.cost();
    const pages = d.toolsList;
    const list: Block[] = pages.map((p, i) => ({
      title: pages.length > 1 ? `tools/list · page ${i + 1}` : 'tools/list',
      json: pretty(p),
      tokens: cost?.pages[i]?.tokens,
      tokensTip: cost ? heaviest(cost.pages[i]?.tools ?? []) : undefined,
    }));
    const instructions = { tokens: cost?.instructions, tokensTip: cost ? 'instructions' : undefined };
    if ('discover' in d) {
      return [{ title: 'server/discover', json: pretty(d.discover), ...instructions }, ...list];
    }
    const handshake = (d as BackendExchange).handshake;
    return [...(handshake ? [{ title: 'initialize', json: pretty(handshake), ...instructions }] : []), ...list];
  });

  protected readonly copied = signal<string | null>(null);

  protected copy(b: Block): void {
    navigator.clipboard?.writeText(b.json).then(
      () => {
        this.copied.set(b.title);
        setTimeout(() => this.copied.set(null), 1500);
      },
      () => undefined,
    );
  }
}
