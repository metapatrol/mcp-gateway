import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { Condition } from '../../api/models';
import { condition } from '../../core/conditions';
import { Lists } from '../../core/lists';
import { ConditionPill } from '../../shared/condition-pill';
import { LoadState } from '../../shared/load-state';
import { SrcTag } from '../../shared/src-tag';

@Component({
  selector: 'app-capabilities',
  imports: [RouterLink, ConditionPill, LoadState, SrcTag],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <header class="mb-5">
      <p class="eyebrow">MCPCapability</p>
      <h1 class="text-2xl font-semibold tracking-tight">Capabilities</h1>
      <p class="muted mt-1">Abilities built from one or more tool sets.</p>
    </header>
    <app-load-state [error]="lists.capabilities.error()" [loading]="!lists.capabilities.hasValue()" what="capabilities" />
    @if (lists.capabilities.hasValue()) {
      <section class="panel overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Description<app-src kind="spec" /></th>
              <th>Tool sets<app-src kind="spec" /></th>
              <th>Valid<app-src kind="status" /></th>
              <th>Ready<app-src kind="status" /></th>
            </tr>
          </thead>
          <tbody>
            @for (c of lists.capabilityItems(); track c.meta.name) {
              <tr class="row-link" (click)="open(c.meta.name)">
                <td><a class="link" [routerLink]="['/capabilities', c.meta.name]">{{ c.meta.name }}</a></td>
                <td class="muted text-[13px]">{{ c.spec.description }}</td>
                <td class="font-mono text-[12.5px]">{{ c.spec.toolSets.join(', ') }}</td>
                <td><app-condition label="Valid" [condition]="get(c.status.conditions, 'Valid')" /></td>
                <td><app-condition label="Ready" [condition]="get(c.status.conditions, 'Ready')" /></td>
              </tr>
            } @empty {
              <tr><td colspan="5" class="faint">No MCPCapability exists.</td></tr>
            }
          </tbody>
        </table>
      </section>
    }
  `,
})
export class Capabilities {
  protected readonly lists = inject(Lists);
  private readonly router = inject(Router);
  protected readonly get = (c: Condition[], t: Condition['type']) => condition(c, t);

  protected open(name: string): void {
    void this.router.navigate(['/capabilities', name]);
  }
}
