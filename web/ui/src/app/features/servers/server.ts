import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { getServer } from '../../api/functions';
import { authRows } from '../../core/auth';
import { condition, CONDITIONS } from '../../core/conditions';
import { Inspect, live } from '../../core/inspect';
import { Lists } from '../../core/lists';
import { ago, shortRevision } from '../../core/time';
import { ConditionPill, Pill } from '../../shared/condition-pill';
import { ConditionsTable } from '../../shared/conditions-table';
import { LoadState } from '../../shared/load-state';
import { McpView } from '../../shared/mcp-view';
import { DetailView, ViewSwitch } from '../../shared/view-switch';
import { ToolDoc } from '../../shared/tool-doc';

@Component({
  selector: 'app-server',
  imports: [RouterLink, ConditionPill, Pill, ConditionsTable, LoadState, ToolDoc, McpView, ViewSwitch],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './server.html',
})
export class ServerDetail {
  protected readonly view = signal<DetailView>('details');
  private readonly inspect = inject(Inspect);
  protected readonly lists = inject(Lists);
  readonly name = input.required<string>();

  protected readonly server = live(
    () => this.name(),
    (name) => this.inspect.call(getServer, { name }),
  );
  protected readonly s = computed(() => (this.server.hasValue() ? this.server.value() : undefined));
  protected readonly auth = computed(() => (this.s() ? authRows(this.s()!.spec.auth) : []));
  protected readonly ready = computed(() => condition(this.s()?.status.conditions, 'Ready'));

  /** Which tool sets expose each backend tool, by their status. */
  protected readonly selectedBy = computed(() => {
    const out = new Map<string, string[]>();
    for (const name of this.s()?.usedBy ?? []) {
      const ts = this.lists.toolSet(name);
      for (const exposed of ts?.status.resolvedTools ?? []) {
        const tool = ts?.status.backendNames?.[exposed] ?? exposed;
        out.set(tool, [...(out.get(tool) ?? []), name]);
      }
    }
    return out;
  });

  protected readonly since = ago;
  protected readonly rev = shortRevision;
  protected readonly toolSetCondition = (name: string) => {
    const ts = this.lists.toolSet(name);
    return CONDITIONS.map((t) => condition(ts?.status.conditions, t)).find((c) => c?.status !== 'True') ?? condition(ts?.status.conditions, 'Ready');
  };
}
