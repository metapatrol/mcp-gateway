import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { Condition } from '../../api/models';
import { condition } from '../../core/conditions';
import { Lists } from '../../core/lists';
import { ConditionPill } from '../../shared/condition-pill';
import { LoadState } from '../../shared/load-state';

@Component({
  selector: 'app-toolsets',
  imports: [RouterLink, ConditionPill, LoadState],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h1 class="mb-4 flex items-baseline gap-2 text-xl font-semibold tracking-tight">
      Tool sets<span class="font-mono text-[13px] font-normal text-faint">MCPToolSet</span>
    </h1>
    <app-load-state [error]="lists.toolSets.error()" [loading]="!lists.toolSets.hasValue()" what="tool sets" />
    @if (lists.toolSets.hasValue()) {
      <section class="panel overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Server</th>
              <th>Selection</th>
              <th class="text-right">Tools</th>
              <th>Valid</th>
              <th>Ready</th>
            </tr>
          </thead>
          <tbody>
            @for (t of lists.toolSetItems(); track t.meta.name) {
              <tr class="row-link" (click)="open(t.meta.name)">
                <td><a class="link" [routerLink]="['/toolsets', t.meta.name]">{{ t.meta.name }}</a></td>
                <td class="font-mono text-[12.5px]">{{ t.spec.serverRef }}</td>
                <td class="muted font-mono text-[12.5px]">
                  {{ t.spec.tools.join(', ') }}
                  @if (t.spec.exclude?.length) {
                    <span class="faint"> − {{ t.spec.exclude.join(', ') }}</span>
                  }
                  @if (t.spec.prefix) {
                    <span class="faint"> · prefix {{ t.spec.prefix }}</span>
                  }
                </td>
                <td class="num">{{ t.status.resolvedToolCount }}</td>
                <td><app-condition label="Valid" [condition]="get(t.status.conditions, 'Valid')" /></td>
                <td><app-condition label="Ready" [condition]="get(t.status.conditions, 'Ready')" /></td>
              </tr>
            } @empty {
              <tr><td colspan="6" class="faint">No MCPToolSet exists.</td></tr>
            }
          </tbody>
        </table>
      </section>
    }
  `,
})
export class ToolSets {
  protected readonly lists = inject(Lists);
  private readonly router = inject(Router);
  protected readonly get = (c: Condition[], t: Condition['type']) => condition(c, t);

  protected open(name: string): void {
    void this.router.navigate(['/toolsets', name]);
  }
}
