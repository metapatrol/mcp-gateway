import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { getGrant } from '../../api/functions';
import { condition } from '../../core/conditions';
import { Inspect, live } from '../../core/inspect';
import { Lists } from '../../core/lists';
import { ConditionPill, Pill } from '../../shared/condition-pill';
import { ConditionsTable } from '../../shared/conditions-table';
import { LoadState } from '../../shared/load-state';
import { SrcTag } from '../../shared/src-tag';

@Component({
  selector: 'app-grant',
  imports: [RouterLink, ConditionPill, Pill, ConditionsTable, LoadState, SrcTag],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <nav class="muted mb-2 flex gap-1.5 text-[12.5px]" aria-label="Breadcrumb">
      <a routerLink="/grants" class="text-accent hover:underline">Grants</a><span class="faint">/</span><span>{{ name() }}</span>
    </nav>
    <app-load-state [error]="grant.error()" [loading]="!grant.hasValue()" what="MCPGrant" [name]="name()" />
    @if (g(); as g) {
      <header class="mb-5 flex flex-wrap items-start justify-between gap-4">
        <div>
          <p class="eyebrow">MCPGrant</p>
          <h1 class="font-mono text-2xl font-semibold tracking-tight">{{ g.meta.name }}</h1>
          <p class="muted mt-1 text-[13.5px]">
            Subject <a class="link" [routerLink]="['/subjects']" [queryParams]="{ subject: g.spec.subject }">{{ g.spec.subject }}</a><app-src kind="spec" />
            @if (g.sameSubject.length) {
              · also granted by
              @for (o of g.sameSubject; track o; let last = $last) {
                <a class="link" [routerLink]="['/grants', o]">{{ o }}</a>@if (!last) {, }
              }
            }
          </p>
        </div>
        <div class="flex flex-wrap gap-1.5">
          <app-condition label="Valid" [condition]="cond(g.status.conditions, 'Valid')" />
          <app-condition label="Ready" [condition]="cond(g.status.conditions, 'Ready')" />
        </div>
      </header>
      <div class="flex flex-col gap-4">
        <section class="panel">
          <header class="panel-head"><h2 class="panel-title">Capabilities<app-src kind="spec" /></h2></header>
          <div class="flex flex-col px-4 py-2">
            @for (name of g.spec.capabilities; track name) {
              <div class="flex flex-wrap items-center justify-between gap-2 py-1.5">
                @if (g.missingCapabilities.includes(name)) {
                  <span>
                    <span class="font-mono text-[13px] text-bad line-through">{{ name }}</span>
                    <span class="ml-2 text-[12.5px] text-bad">does not exist, so it grants nothing</span>
                  </span>
                  <app-pill tone="bad">missing</app-pill>
                } @else {
                  <span>
                    <a class="link" [routerLink]="['/capabilities', name]">{{ name }}</a>
                    <span class="muted ml-2 text-[12.5px]">{{ lists.capability(name)?.spec?.description }}</span>
                  </span>
                  <app-condition label="Ready" [condition]="cond(lists.capability(name)?.status?.conditions, 'Ready')" />
                }
              </div>
            }
          </div>
        </section>
        <app-conditions [conditions]="g.status.conditions" [observedGeneration]="g.status.observedGeneration" [generation]="g.meta.generation" />
      </div>
    }
  `,
})
export class GrantDetail {
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
