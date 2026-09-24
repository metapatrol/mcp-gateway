import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { getSubject } from '../../api/functions';
import { SubjectTool } from '../../api/models';
import { Inspect, live } from '../../core/inspect';
import { Lists } from '../../core/lists';
import { Pill } from '../../shared/condition-pill';
import { LoadState } from '../../shared/load-state';
import { ToolDoc } from '../../shared/tool-doc';
import { byServer, notOffered } from './grouping';

@Component({
  selector: 'app-subject',
  imports: [RouterLink, Pill, LoadState, ToolDoc],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './subject.html',
})
export class SubjectDetail {
  private readonly inspect = inject(Inspect);
  private readonly lists = inject(Lists);

  /** From the query parameter: a subject may be a URL. */
  readonly subject = input<string>();

  protected readonly resolved = live(
    () => this.subject() || undefined,
    (subject) => this.inspect.call(getSubject, { subject }),
  );
  protected readonly r = computed(() => (this.resolved.hasValue() ? this.resolved.value() : undefined));
  protected readonly groups = computed(() => byServer(this.r()?.tools ?? []));
  protected readonly missing = computed(() => notOffered(this.r()?.tools ?? []));
  protected readonly offered = computed(() => this.groups().reduce((n, g) => n + g.tools.length, 0));
  protected readonly notFound = computed(() => {
    const r = this.r();
    if (!r) {
      return [];
    }
    return [
      ...r.missingCapabilities.map((n) => `capability ${n}`),
      ...r.missingToolSets.map((n) => `tool set ${n}`),
      ...r.missingServers.map((n) => `server ${n}`),
    ];
  });

  /** Why a resolved tool is not offered, in a few words. */
  protected why(t: SubjectTool): string {
    if (t.state === 'conflict') {
      return `name claimed by ${t.claimants.map((c) => `${c.ref.server}/${c.ref.tool}`).join(' and ')}`;
    }
    const server = t.claimants[0]?.ref.server;
    return this.lists.server(server ?? '')?.discovery.usable ? `${server} does not offer it` : `${server} is not served right now`;
  }
}
