import { booleanAttribute, ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { Condition } from '../api/models';
import { tone } from '../core/conditions';
import { ago } from '../core/time';
import { Tip } from './tip';

export const PILL: Record<string, string> = {
  ok: 'bg-ok-soft text-ok',
  bad: 'bg-bad-soft text-bad',
  warn: 'bg-warn-soft text-warn',
  neutral: 'bg-sunken text-muted',
};

/** A condition as a pill: its type, its reason; its message and age on hover. */
@Component({
  selector: 'app-condition',
  imports: [Tip],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <span
      class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap"
      [class]="classes()"
      [appTip]="tip()"
      [focusable]="true"
    >
      <span class="size-1.5 rounded-full bg-current" aria-hidden="true"></span>
      @if (!bare()) {
        {{ label() }}
      }
      <span [class.font-normal]="!bare()" [class.opacity-80]="!bare()">{{ condition()?.reason ?? 'Unknown' }}</span>
    </span>
  `,
})
export class ConditionPill {
  readonly condition = input<Condition | undefined>();
  readonly label = input.required<string>();
  /** Only the reason, for a table column whose header already names the condition. */
  readonly bare = input(false, { transform: booleanAttribute });
  protected readonly classes = computed(() => PILL[tone(this.condition())]);
  protected readonly tip = computed(() => {
    const c = this.condition();
    return c ? `${c.message}\n${c.type} ${c.status} since ${ago(c.lastTransitionTime)}` : 'Not reported yet.';
  });
}

/** A plain pill for a state that is not a condition. */
@Component({
  selector: 'app-pill',
  imports: [Tip],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <span
      class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap"
      [class]="classes()"
      [appTip]="tip()"
      [focusable]="!!tip()"
    >
      <span class="size-1.5 rounded-full bg-current" aria-hidden="true"></span>
      <ng-content />
    </span>
  `,
})
export class Pill {
  readonly tone = input<'ok' | 'bad' | 'warn' | 'neutral'>('neutral');
  /** What is behind the pill, on hover and focus. */
  readonly tip = input<string | undefined>();
  protected readonly classes = computed(() => PILL[this.tone()]);
}
