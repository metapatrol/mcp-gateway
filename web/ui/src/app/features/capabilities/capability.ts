import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { getCapability } from '../../api/functions';
import { condition } from '../../core/conditions';
import { Inspect, live } from '../../core/inspect';
import { Lists } from '../../core/lists';
import { ConditionPill, Pill } from '../../shared/condition-pill';
import { ConditionsTable } from '../../shared/conditions-table';
import { InstructionsPanel } from '../../shared/instructions-panel';
import { DetailTitle } from '../../shared/detail-title';
import { LoadState } from '../../shared/load-state';
import { Notice } from '../../shared/notice';
import { McpView } from '../../shared/mcp-view';
import { DetailView, ViewSwitch } from '../../shared/view-switch';

@Component({
  selector: 'app-capability',
  imports: [RouterLink, ConditionPill, Pill, ConditionsTable, LoadState, McpView, ViewSwitch, InstructionsPanel, Notice, DetailTitle],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <header class="mb-3 flex flex-wrap items-start justify-between gap-x-4 gap-y-1">
      <div class="min-w-0">
        <app-detail-title section="Capabilities" link="/capabilities" [name]="name()" />
        @if (c(); as c) {
          @if (c.spec.description) {
            <p class="muted mt-0.5 text-[13px]">{{ c.spec.description }}</p>
          }
        }
      </div>
      @if (c(); as c) {
        <div class="flex flex-wrap gap-1.5">
          <app-condition label="Valid" [condition]="cond(c.status.conditions, 'Valid')" />
          <app-condition label="Ready" [condition]="cond(c.status.conditions, 'Ready')" />
        </div>
      }
    </header>
    <app-load-state [error]="capability.error()" [loading]="!capability.hasValue()" what="MCPCapability" [name]="name()" />
    @if (c(); as c) {
      <app-view-switch [(view)]="view" />

      @if (view() === 'mcp') {
        <app-mcp-view kind="capability" [name]="name()" />
      } @else {
      <div class="flex flex-col gap-4">
        @if (c.missingToolSets.length) {
          <app-notice tone="bad">Not found: {{ c.missingToolSets.join(', ') }}. They contribute nothing.</app-notice>
        }
        @if (c.conflicts.length) {
          <section class="panel border-bad/40">
            <header class="panel-head"><h2 class="panel-title text-bad">Name conflicts</h2></header>
            <div class="overflow-x-auto">
              <table class="data-table">
                <thead><tr><th>Name</th><th>Claimed by</th></tr></thead>
                <tbody>
                  @for (x of c.conflicts; track x.name) {
                    <tr>
                      <td class="font-mono">{{ x.name }}</td>
                      <td>
                        @for (cl of x.claimants; track cl.ref.server + cl.ref.tool) {
                          <div>
                            <a class="link" [routerLink]="['/servers', cl.ref.server]">{{ cl.ref.server }}</a>
                            <span class="muted font-mono text-[12.5px]">/{{ cl.ref.tool }}</span>
                            <span class="faint text-xs"> via {{ cl.toolSets.join(', ') }}</span>
                          </div>
                        }
                      </td>
                    </tr>
                  }
                </tbody>
              </table>
            </div>
          </section>
        }

        <section class="panel">
          <header class="panel-head">
            <h2 class="panel-title">Tools</h2>
            <span class="faint text-xs">{{ c.tools.length }} from {{ servers() }} {{ servers() === 1 ? 'server' : 'servers' }}</span>
          </header>
          <div class="overflow-x-auto">
            <table class="data-table">
              <thead><tr><th>Agents see</th><th>Backend tool</th><th>Tool sets</th><th>Offered</th></tr></thead>
              <tbody>
                @for (t of c.tools; track t.name) {
                  <tr>
                    <td class="font-mono text-[13px]">{{ t.name }}</td>
                    <td><a class="link" [routerLink]="['/servers', t.ref.server]">{{ t.ref.server }}</a><span class="muted font-mono text-[12.5px]">/{{ t.ref.tool }}</span></td>
                    <td>
                      @for (ts of t.toolSets; track ts; let last = $last) {
                        <a class="link" [routerLink]="['/toolsets', ts]">{{ ts }}</a>@if (!last) {, }
                      }
                    </td>
                    <td>
                      <app-pill [tone]="t.available ? 'ok' : 'warn'">{{ t.available ? 'yes' : 'not right now' }}</app-pill>
                    </td>
                  </tr>
                } @empty {
                  <tr><td colspan="4" class="faint">This capability comes to no tools.</td></tr>
                }
              </tbody>
            </table>
          </div>
        </section>

        <app-instructions [text]="c.instructions" />

        <div class="grid gap-4 xl:grid-cols-2">
          <section class="panel">
            <header class="panel-head"><h2 class="panel-title">Tool sets</h2></header>
            <div class="flex flex-col px-4 py-2">
              @for (t of c.spec.toolSets; track t) {
                <div class="flex flex-wrap items-center justify-between gap-2 py-1.5">
                  @if (c.missingToolSets.includes(t)) {
                    <span class="font-mono text-[13px] text-bad line-through">{{ t }}</span>
                    <app-pill tone="bad">does not exist</app-pill>
                  } @else {
                    <a class="link" [routerLink]="['/toolsets', t]">{{ t }}</a>
                    <app-condition label="Ready" [condition]="toolSetReady(t)" />
                  }
                </div>
              }
            </div>
          </section>
          <section class="panel">
            <header class="panel-head"><h2 class="panel-title">Granted by</h2></header>
            <div class="flex flex-col px-4 py-2">
              @for (g of c.grantedBy; track g) {
                <div class="flex flex-wrap items-center justify-between gap-2 py-1.5">
                  <a class="link" [routerLink]="['/grants', g]">{{ g }}</a>
                  <span class="muted font-mono text-xs">{{ subjectOf(g) }}</span>
                </div>
              } @empty {
                <p class="faint py-1 text-sm">No grant uses this capability.</p>
              }
            </div>
          </section>
        </div>

        <app-conditions [conditions]="c.status.conditions" [observedGeneration]="c.status.observedGeneration" [generation]="c.meta.generation" />
      </div>
      }
    }
  `,
})
export class CapabilityDetail {
  protected readonly view = signal<DetailView>('details');
  private readonly inspect = inject(Inspect);
  private readonly lists = inject(Lists);
  readonly name = input.required<string>();

  protected readonly capability = live(
    () => this.name(),
    (name) => this.inspect.call(getCapability, { name }),
  );
  protected readonly c = computed(() => (this.capability.hasValue() ? this.capability.value() : undefined));
  protected readonly servers = computed(() => new Set(this.c()?.tools.map((t) => t.ref.server)).size);
  protected readonly cond = condition;

  protected toolSetReady(name: string) {
    return condition(this.lists.toolSet(name)?.status.conditions, 'Ready');
  }

  protected subjectOf(grant: string): string {
    return this.lists.grantItems().find((g) => g.meta.name === grant)?.spec.subject ?? '';
  }
}
