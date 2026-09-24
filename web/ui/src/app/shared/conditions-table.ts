import { ChangeDetectionStrategy, Component, input } from '@angular/core';
import { Condition } from '../api/models';
import { ago } from '../core/time';
import { ConditionPill } from './condition-pill';

@Component({
  selector: 'app-conditions',
  imports: [ConditionPill],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="panel">
      <header class="panel-head">
        <h2 class="panel-title">Conditions</h2>
        @if (observedGeneration() !== undefined) {
          <span class="faint text-xs">observed generation {{ observedGeneration() }} of {{ generation() }}</span>
        }
      </header>
      @if (conditions().length) {
        <div class="overflow-x-auto">
          <table class="data-table">
            <thead>
              <tr><th>Type</th><th>Status</th><th>Message</th><th>Since</th></tr>
            </thead>
            <tbody>
              @for (c of conditions(); track c.type) {
                <tr>
                  <td>{{ c.type }}</td>
                  <td><app-condition [label]="c.status" [condition]="c" /></td>
                  <td class="muted">{{ c.message }}</td>
                  <td class="faint whitespace-nowrap">{{ since(c.lastTransitionTime) }}</td>
                </tr>
              }
            </tbody>
          </table>
        </div>
      } @else {
        <p class="faint px-4 py-3 text-sm">Not reported yet.</p>
      }
    </section>
  `,
})
export class ConditionsTable {
  readonly conditions = input.required<Condition[]>();
  readonly observedGeneration = input<number | undefined>();
  readonly generation = input<number | undefined>();
  protected readonly since = ago;
}
