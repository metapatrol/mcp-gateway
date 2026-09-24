import { ChangeDetectionStrategy, Component, computed, effect, inject, input, signal } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { getSubject, listSubjects } from '../../api/functions';
import { SubjectTool } from '../../api/models';
import { Inspect, live } from '../../core/inspect';
import { Lists } from '../../core/lists';
import { Pill } from '../../shared/condition-pill';
import { LoadState } from '../../shared/load-state';
import { SrcTag } from '../../shared/src-tag';
import { ToolDoc } from '../../shared/tool-doc';

@Component({
  selector: 'app-subjects',
  imports: [RouterLink, Pill, LoadState, SrcTag, ToolDoc],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './subjects.html',
})
export class Subjects {
  private readonly inspect = inject(Inspect);
  private readonly router = inject(Router);
  private readonly lists = inject(Lists);

  /** From the query parameter: a subject may be a URL. */
  readonly subject = input<string>();

  protected readonly all = live(
    () => ({}),
    () => this.inspect.call(listSubjects, {}),
  );
  protected readonly resolved = live(
    () => this.subject() || undefined,
    (subject) => this.inspect.call(getSubject, { subject }),
  );
  protected readonly r = computed(() => (this.resolved.hasValue() ? this.resolved.value() : undefined));
  protected readonly offered = computed(() => this.r()?.tools.filter((t) => t.state === 'offered').length ?? 0);

  protected readonly typed = signal('');

  constructor() {
    effect(() => this.typed.set(this.subject() ?? ''));
  }

  protected pick(subject: string): void {
    void this.router.navigate(['/subjects'], { queryParams: { subject: subject || null } });
  }

  protected submit(event: Event): void {
    event.preventDefault();
    this.pick(this.typed().trim());
  }

  /**
   * Why a resolved tool is not offered: its server's catalog is not served,
   * or the catalog is served and lacks the tool — a name a tool set lists
   * that the backend does not have.
   */
  protected unavailableBecause(t: SubjectTool): string {
    const server = t.claimants[0]?.ref.server;
    const usable = server ? this.lists.server(server)?.discovery.usable : undefined;
    return usable ? `${server} does not offer it` : `${server} is not served right now`;
  }

  protected rowClass(t: SubjectTool): string {
    switch (t.state) {
      case 'conflict':
        return 'bg-bad-soft';
      case 'unavailable':
        return 'opacity-70';
      default:
        return '';
    }
  }
}
