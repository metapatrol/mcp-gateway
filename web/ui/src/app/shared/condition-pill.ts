import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { Condition } from '../api/models';
import { tone } from '../core/conditions';

export const PILL: Record<string, string> = {
  ok: 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/60 dark:text-emerald-300',
  bad: 'bg-red-50 text-red-700 dark:bg-red-950/60 dark:text-red-300',
  warn: 'bg-amber-50 text-amber-700 dark:bg-amber-950/60 dark:text-amber-300',
  neutral: 'bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-400',
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
