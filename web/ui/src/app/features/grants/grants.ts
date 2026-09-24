import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { Condition } from '../../api/models';
import { condition } from '../../core/conditions';
import { Lists } from '../../core/lists';
import { ConditionPill } from '../../shared/condition-pill';
import { LoadState } from '../../shared/load-state';

@Component({
  selector: 'app-grants',
  imports: [RouterLink, ConditionPill, LoadState],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h1 class="mb-4 flex items-baseline gap-2 text-xl font-semibold tracking-tight">
      Grants<span class="font-mono text-[13px] font-normal text-faint">MCPGrant</span>
    </h1>
    <app-load-state [error]="lists.grants.error()" [loading]="!lists.grants.hasValue()" what="grants" />
    @if (lists.grants.hasValue()) {
      <section class="panel overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Subject</th>
              <th>Capabilities</th>
              <th>Valid</th>
              <th>Ready</th>
            </tr>
          </thead>
          <tbody>
            @for (g of lists.grantItems(); track g.meta.name) {
              <tr class="row-link" (click)="open(g.meta.name)">
                <td><a class="link" [routerLink]="['/grants', g.meta.name]">{{ g.meta.name }}</a></td>
                <td>
                  <a class="link" [routerLink]="['/subjects/resolve']" [queryParams]="{ subject: g.spec.subject }" (click)="$event.stopPropagation()">{{ g.spec.subject }}</a>
                </td>
                <td class="font-mono text-[12.5px]">{{ g.spec.capabilities.join(', ') }}</td>
                <td><app-condition label="Valid" [condition]="get(g.status.conditions, 'Valid')" /></td>
                <td><app-condition label="Ready" [condition]="get(g.status.conditions, 'Ready')" /></td>
              </tr>
            } @empty {
              <tr><td colspan="5" class="faint">No MCPGrant exists.</td></tr>
            }
          </tbody>
        </table>
      </section>
    }
  `,
})
export class Grants {
  protected readonly lists = inject(Lists);
  private readonly router = inject(Router);
  protected readonly get = (c: Condition[], t: Condition['type']) => condition(c, t);

  protected open(name: string): void {
    void this.router.navigate(['/grants', name]);
  }
}
