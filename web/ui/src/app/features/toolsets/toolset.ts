import { ChangeDetectionStrategy, Component, computed, effect, inject, input, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { getToolSet } from '../../api/functions';
import { condition } from '../../core/conditions';
import { Inspect, live } from '../../core/inspect';
import { Lists } from '../../core/lists';
import { shortRevision } from '../../core/time';
import { ConditionPill } from '../../shared/condition-pill';
import { ConditionsTable } from '../../shared/conditions-table';
import { LoadState } from '../../shared/load-state';
import { ToolDoc } from '../../shared/tool-doc';
import { disagreement, Filter, filtered, reason, touchedBy } from './selection';

@Component({
  selector: 'app-toolset',
  imports: [RouterLink, ConditionPill, ConditionsTable, LoadState, ToolDoc],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './toolset.html',
})
export class ToolSetDetail {
  private readonly inspect = inject(Inspect);
  protected readonly lists = inject(Lists);
  readonly name = input.required<string>();

  protected readonly toolSet = live(
    () => this.name(),
    (name) => this.inspect.call(getToolSet, { name }),
  );
  protected readonly t = computed(() => (this.toolSet.hasValue() ? this.toolSet.value() : undefined));

  protected readonly filter = signal<Filter>('all');
  protected readonly focus = signal<string | null>(null);

  protected readonly rows = computed(() => {
    const t = this.t();
    return t ? filtered(t.selection, this.filter()) : [];
  });
  protected readonly counts = computed(() => {
    const tools = this.t()?.selection.tools ?? [];
    const exposed = tools.filter((x) => x.state === 'selected').length;
    return { all: tools.length, exposed, leftOut: tools.length - exposed };
  });
  protected readonly differs = computed(() => {
    const t = this.t();
    return t && t.selection.catalogAvailable ? disagreement(t.status.resolvedTools, t.selection) : [];
  });
  protected readonly serverReady = computed(() => {
    const t = this.t();
    return t ? condition(this.lists.server(t.spec.serverRef)?.status.conditions, 'Ready') : undefined;
  });

  protected readonly cond = condition;
  protected readonly reason = reason;
  protected readonly touched = touchedBy;
  protected readonly rev = shortRevision;
  protected readonly aliasEntries = computed(() => this.t()?.selection.aliases ?? []);

  constructor() {
    // A different tool set starts unfiltered.
    effect(() => {
      this.name();
      this.focus.set(null);
      this.filter.set('all');
    });
  }

  protected toggleFocus(entry: string): void {
    this.focus.update((f) => (f === entry ? null : entry));
  }
}
