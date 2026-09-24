import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { getToolSet } from '../../api/functions';
import { condition } from '../../core/conditions';
import { Inspect, live } from '../../core/inspect';
import { Lists } from '../../core/lists';
import { queryParams } from '../../core/query';
import { shortRevision } from '../../core/time';
import { ConditionPill } from '../../shared/condition-pill';
import { ConditionsTable } from '../../shared/conditions-table';
import { InstructionsPanel } from '../../shared/instructions-panel';
import { DetailTitle } from '../../shared/detail-title';
import { LoadState } from '../../shared/load-state';
import { Notice } from '../../shared/notice';
import { McpView } from '../../shared/mcp-view';
import { detailView, ViewSwitch } from '../../shared/view-switch';
import { ToolDoc } from '../../shared/tool-doc';
import { disagreement, Filter, filtered, reason, touchedBy } from './selection';

@Component({
  selector: 'app-toolset',
  imports: [RouterLink, ConditionPill, ConditionsTable, LoadState, ToolDoc, McpView, ViewSwitch, InstructionsPanel, Notice, DetailTitle],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './toolset.html',
})
export class ToolSetDetail {
  /** The route's optional /mcp segment. */
  readonly tab = input<string>();
  protected readonly view = computed(() => detailView(this.tab()));
  private readonly inspect = inject(Inspect);
  protected readonly lists = inject(Lists);
  readonly name = input.required<string>();

  protected readonly toolSet = live(
    () => this.name(),
    (name) => this.inspect.call(getToolSet, { name }),
  );
  protected readonly t = computed(() => (this.toolSet.hasValue() ? this.toolSet.value() : undefined));

  /** ?filter= and ?focus=, so a selection can be linked to as it is shown. */
  readonly filter = input<string>();
  readonly focus = input<string>();
  protected readonly shown = computed<Filter>(() => {
    const f = this.filter();
    return f === 'exposed' || f === 'left-out' ? f : 'all';
  });
  protected readonly focused = computed(() => this.focus() || null);
  private readonly setQuery = queryParams();

  protected readonly rows = computed(() => {
    const t = this.t();
    return t ? filtered(t.selection, this.shown()) : [];
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

  protected setFilter(filter: Filter): void {
    this.setQuery({ filter: filter === 'all' ? null : filter });
  }

  protected toggleFocus(entry: string): void {
    this.setQuery({ focus: this.focused() === entry ? null : entry });
  }
}
