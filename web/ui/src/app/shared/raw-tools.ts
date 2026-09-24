import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { listCapabilityTools, listGrantTools, listServerTools, listToolSetTools } from '../api/functions';
import { Inspect, live } from '../core/inspect';
import { LoadState } from './load-state';

export type ToolsOwner = 'server' | 'toolset' | 'capability' | 'grant';

/** What a resource hands out, as the tools/list result an MCP client gets. */
@Component({
  selector: 'app-raw-tools',
  imports: [LoadState],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <app-load-state [error]="doc.error()" [loading]="!doc.hasValue()" what="tools/list" />
    @if (json(); as text) {
      <section class="panel">
        <header class="panel-head">
          <h2 class="panel-title">tools/list</h2>
          <div class="flex items-center gap-3">
            <span class="faint text-xs">{{ count() }} {{ count() === 1 ? 'tool' : 'tools' }}</span>
            <button type="button" class="btn" (click)="copy(text)">{{ copied() ? 'Copied' : 'Copy' }}</button>
          </div>
        </header>
        <pre class="max-h-[75vh] overflow-auto px-4 py-3 font-mono text-[12.5px] leading-relaxed">{{ text }}</pre>
      </section>
    }
  `,
})
export class RawTools {
  private readonly inspect = inject(Inspect);
  readonly kind = input.required<ToolsOwner>();
  readonly name = input.required<string>();

  protected readonly doc = live(
    () => ({ kind: this.kind(), name: this.name() }),
    ({ kind, name }) => {
      switch (kind) {
        case 'server':
          return this.inspect.api.invoke(listServerTools, { name });
        case 'toolset':
          return this.inspect.api.invoke(listToolSetTools, { name });
        case 'capability':
          return this.inspect.api.invoke(listCapabilityTools, { name });
        default:
          return this.inspect.api.invoke(listGrantTools, { name });
      }
    },
  );
  protected readonly json = computed(() => (this.doc.hasValue() && this.doc.value() ? JSON.stringify(this.doc.value(), null, 2) : undefined));
  protected readonly count = computed(() => (this.doc.hasValue() ? (this.doc.value()?.tools.length ?? 0) : 0));
  protected readonly copied = signal(false);

  protected copy(text: string): void {
    navigator.clipboard?.writeText(text).then(
      () => {
        this.copied.set(true);
        setTimeout(() => this.copied.set(false), 1500);
      },
      () => undefined,
    );
  }
}
