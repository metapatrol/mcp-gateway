import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { getCapabilityMcp, getGrantMcp, getServerMcp, getSubjectMcp, getToolSetMcp } from '../api/functions';
import { AgentExchange, BackendExchange } from '../api/models';
import { Inspect, live } from '../core/inspect';
import { JsonViewer } from './json-viewer';
import { LoadState } from './load-state';

export type McpOwner = 'server' | 'toolset' | 'capability' | 'grant' | 'subject';

interface Block {
  title: string;
  json: string;
}

const pretty = (v: unknown) => JSON.stringify(v, null, 2);

/**
 * What a resource comes to over MCP, as JSON: for a server, what the backend
 * answered the gateway; for the others, the gateway's own answers to
 * server/discover and tools/list for an agent holding the resource.
 */
@Component({
  selector: 'app-mcp-view',
  imports: [LoadState, JsonViewer],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <app-load-state [error]="doc.error()" [loading]="!doc.hasValue()" what="MCP view" />
    <div class="flex flex-col gap-4">
      @for (b of blocks(); track b.title) {
        <section class="panel">
          <header class="panel-head">
            <h2 class="panel-title font-mono tracking-normal normal-case">{{ b.title }}</h2>
            <button type="button" class="btn" (click)="copy(b)">{{ copied() === b.title ? 'Copied' : 'Copy' }}</button>
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

  protected readonly blocks = computed<Block[]>(() => {
    const d = this.doc.hasValue() ? this.doc.value() : undefined;
    if (!d) {
      return [];
    }
    const pages = d.toolsList;
    const list = pages.map((p, i) => ({ title: pages.length > 1 ? `tools/list · page ${i + 1}` : 'tools/list', json: pretty(p) }));
    if ('discover' in d) {
      return [{ title: 'server/discover', json: pretty(d.discover) }, ...list];
    }
    const handshake = (d as BackendExchange).handshake;
    return [...(handshake ? [{ title: 'initialize', json: pretty(handshake) }] : []), ...list];
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
