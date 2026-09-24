import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { condition } from '../../core/conditions';
import { Lists } from '../../core/lists';
import { ago } from '../../core/time';
import { ConditionPill } from '../../shared/condition-pill';
import { LoadState } from '../../shared/load-state';

@Component({
  selector: 'app-servers',
  imports: [RouterLink, ConditionPill, LoadState],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h1 class="mb-4 flex items-baseline gap-2 text-xl font-semibold tracking-tight">
      Servers<span class="font-mono text-[13px] font-normal text-faint">MCPServer</span>
    </h1>
    <app-load-state [error]="lists.servers.error()" [loading]="!lists.servers.hasValue()" what="servers" />
    @if (lists.servers.hasValue()) {
      <section class="panel overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Endpoint</th>
              <th>Auth</th>
              <th class="text-right">Tools</th>
              <th>Last discovery</th>
              <th>Ready</th>
            </tr>
          </thead>
          <tbody>
            @for (s of lists.serverItems(); track s.meta.name) {
              <tr class="row-link" (click)="open(s.meta.name)">
                <td><a class="link" [routerLink]="['/servers', s.meta.name]">{{ s.meta.name }}</a></td>
                <td class="muted font-mono text-[12.5px]">{{ s.spec.endpoint }}</td>
                <td>{{ s.spec.auth.type }}</td>
                <td class="num">{{ s.discovery.toolCount ?? '—' }}</td>
                <td class="muted whitespace-nowrap">
                  {{ since(s.discovery.lastSuccess) }}
                  @if (s.discovery.consecutiveFailures) {
                    <span class="text-bad"> · {{ s.discovery.consecutiveFailures }} failed</span>
                  }
                </td>
                <td><app-condition label="Ready" [condition]="ready(s.status.conditions)" /></td>
              </tr>
            } @empty {
              <tr><td colspan="6" class="faint">No MCPServer exists.</td></tr>
            }
          </tbody>
        </table>
      </section>
    }
  `,
})
export class Servers {
  protected readonly lists = inject(Lists);
  private readonly router = inject(Router);
  protected readonly since = ago;
  protected readonly ready = (c: Parameters<typeof condition>[0]) => condition(c, 'Ready');

  protected open(name: string): void {
    void this.router.navigate(['/servers', name]);
  }
}
