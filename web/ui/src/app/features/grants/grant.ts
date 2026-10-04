import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { MpCondition, MpConditions, MpDetailTitle, MpLoadState, MpNotice, MpPill } from '@metapatrol/ng';
import { getGrant } from '../../api/functions';
import { condition } from '../../core/conditions';
import { Inspect, live } from '../../core/inspect';
import { Lists } from '../../core/lists';
import { InstructionsPanel } from '../../shared/instructions-panel';
import { McpView } from '../../shared/mcp-view';
import { detailView, ViewSwitch } from '../../shared/view-switch';

@Component({
  selector: 'app-grant',
  imports: [RouterLink, MpCondition, MpPill, MpConditions, MpLoadState, McpView, ViewSwitch, InstructionsPanel, MpNotice, MpDetailTitle],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <header class="mb-3 flex flex-wrap items-start justify-between gap-x-4 gap-y-1">
      <div class="min-w-0">
        <mp-detail-title section="Grants" link="/grants" [name]="name()" />
        @if (g(); as g) {
          <p class="text-fg-muted mt-0.5 text-[13px]">
            subject <a class="mp-link" [routerLink]="['/subjects/resolve']" [queryParams]="{ subject: g.spec.subject }">{{ g.spec.subject }}</a>
            @if (g.sameSubject.length) {
              · also granted by
              @for (o of g.sameSubject; track o; let last = $last) {
                <a class="mp-link" [routerLink]="['/grants', o]">{{ o }}</a>@if (!last) {, }
              }
            }
          </p>
        }
      </div>
      @if (g(); as g) {
        <div class="flex flex-wrap gap-1.5">
          <mp-condition label="Valid" [condition]="cond(g.status.conditions, 'Valid')" />
          <mp-condition label="Ready" [condition]="cond(g.status.conditions, 'Ready')" />
        </div>
      }
    </header>
    <mp-load-state [error]="grant.error()" [loading]="!grant.hasValue()" what="MCPGrant" [name]="name()" />
    @if (g(); as g) {
      <app-view-switch [view]="view()" [base]="['/grants', name()]" />
      @if (view() === 'mcp') {
        <app-mcp-view kind="grant" [name]="name()" />
      } @else {
      <div class="flex flex-col gap-4">
        @if (g.missingCapabilities.length) {
          <mp-notice tone="bad">Not found: {{ g.missingCapabilities.join(', ') }}. They grant nothing.</mp-notice>
        }
        <section class="mp-panel mp-panel-flush">
          <header class="mp-panel-head"><h2 class="mp-label">Capabilities</h2></header>
          <div class="flex flex-col px-4 py-2">
            @for (name of g.spec.capabilities; track name) {
              <div class="flex flex-wrap items-center justify-between gap-2 py-1.5">
                @if (g.missingCapabilities.includes(name)) {
                  <span class="font-mono text-[13px] text-error line-through">{{ name }}</span>
                  <mp-pill tone="bad">not found</mp-pill>
                } @else {
                  <span>
                    <a class="mp-link" [routerLink]="['/capabilities', name]">{{ name }}</a>
                    <span class="text-fg-muted ml-2 text-[12.5px]">{{ lists.capability(name)?.spec?.description }}</span>
                  </span>
                  <mp-condition label="Ready" [condition]="cond(lists.capability(name)?.status?.conditions, 'Ready')" />
                }
              </div>
            }
          </div>
        </section>
        <app-instructions [text]="g.instructions" />

        <mp-conditions [conditions]="g.status.conditions" [observedGeneration]="g.status.observedGeneration" [generation]="g.meta.generation" />
      </div>
      }
    }
  `,
})
export class GrantDetail {
  /** The route's optional /mcp segment. */
  readonly tab = input<string>();
  protected readonly view = computed(() => detailView(this.tab()));
  private readonly inspect = inject(Inspect);
  protected readonly lists = inject(Lists);
  readonly name = input.required<string>();

  protected readonly grant = live(
    () => this.name(),
    (name) => this.inspect.call(getGrant, { name }),
  );
  protected readonly g = computed(() => (this.grant.hasValue() ? this.grant.value() : undefined));
  protected readonly cond = condition;
}
