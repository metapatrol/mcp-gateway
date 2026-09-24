import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { Condition } from '../../api/models';
import { condition } from '../../core/conditions';
import { Lists } from '../../core/lists';
import { ConditionPill } from '../../shared/condition-pill';
import { LoadState } from '../../shared/load-state';

@Component({
  selector: 'app-capabilities',
  imports: [RouterLink, ConditionPill, LoadState],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h1 class="mb-4 flex items-baseline gap-2 text-xl font-semibold tracking-tight">
      Capabilities<span class="font-mono text-[13px] font-normal text-faint">MCPCapability</span>
    </h1>
    <app-load-state [error]="lists.capabilities.error()" [loading]="!lists.capabilities.hasValue()" what="capabilities" />
    @if (lists.capabilities.hasValue()) {
      <section class="panel overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Description</th>
              <th>Tool sets</th>
              <th>Valid</th>
              <th>Ready</th>
            </tr>
          </thead>
          <tbody>
            @for (c of lists.capabilityItems(); track c.meta.name) {
              <tr class="row-link" (click)="open(c.meta.name)">
                <td><a class="link" [routerLink]="['/capabilities', c.meta.name]">{{ c.meta.name }}</a></td>
                <td class="muted text-[13px]">{{ c.spec.description }}</td>
                <td>
                  @for (t of c.spec.toolSets; track t; let last = $last) {
                    @if (lists.toolSet(t)) {
                      <a class="link text-[12.5px]" [routerLink]="['/toolsets', t]" (click)="$event.stopPropagation()">{{ t }}</a>
                    } @else {
                      <span class="font-mono text-[12.5px] text-bad line-through" title="not found">{{ t }}</span>
                    }@if (!last) {, }
                  }
                </td>
                <td><app-condition label="Valid" bare [condition]="get(c.status.conditions, 'Valid')" /></td>
                <td><app-condition label="Ready" bare [condition]="get(c.status.conditions, 'Ready')" /></td>
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
