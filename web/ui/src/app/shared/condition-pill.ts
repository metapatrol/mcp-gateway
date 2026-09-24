import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { Condition } from '../api/models';
import { tone } from '../core/conditions';

export const PILL: Record<string, string> = {
  ok: 'bg-ok-soft text-ok',
  bad: 'bg-bad-soft text-bad',
  warn: 'bg-warn-soft text-warn',
  neutral: 'bg-sunken text-muted',
};

/** A condition as a pill: its type, its reason, the message on hover. */
@Component({
  selector: 'app-condition',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <span
      class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap"
      [class]="classes()"
      [title]="condition()?.message ?? 'not reported yet'"
    >
      <span class="size-1.5 rounded-full bg-current" aria-hidden="true"></span>
      {{ label() }}
      <span class="font-normal opacity-80">{{ condition()?.reason ?? 'Unknown' }}</span>
    </span>
  `,
})
export class ConditionPill {
  readonly condition = input<Condition | undefined>();
  readonly label = input.required<string>();
  protected readonly classes = computed(() => PILL[tone(this.condition())]);
}

/** A plain pill for a state that is not a condition. */
@Component({
  selector: 'app-pill',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <span class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap" [class]="classes()">
      <span class="size-1.5 rounded-full bg-current" aria-hidden="true"></span>
      <ng-content />
    </span>
  `,
})
export class Pill {
  readonly tone = input<'ok' | 'bad' | 'warn' | 'neutral'>('neutral');
  protected readonly classes = computed(() => PILL[this.tone()]);
}
