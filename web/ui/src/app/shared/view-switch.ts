import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { Params, RouterLink } from '@angular/router';

export type DetailView = 'details' | 'mcp';

/** The view a detail route's optional /mcp segment names. */
export const detailView = (segment: string | undefined): DetailView => (segment === 'mcp' ? 'mcp' : 'details');

/**
 * Tabs between a resource's page and what it comes to over MCP, as JSON. Each
 * is a link: the MCP view is the page's path with /mcp, as in the API.
 */
@Component({
  selector: 'app-view-switch',
  imports: [RouterLink],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <nav class="mb-4 flex gap-4 border-b border-line text-[13px]">
      @for (t of tabs(); track t.key) {
        <a
          class="-mb-px border-b-2 px-0.5 pb-2"
          [class]="view() === t.key ? 'border-accent font-semibold text-ink' : 'border-transparent text-muted hover:text-ink'"
          [attr.aria-current]="view() === t.key ? 'page' : null"
          [routerLink]="t.link"
          [queryParams]="query()"
        >
          {{ t.label }}
        </a>
      }
    </nav>
  `,
})
export class ViewSwitch {
  readonly view = input.required<DetailView>();
  /** The page's path, without /mcp. */
  readonly base = input.required<string[]>();
  /** Query parameters that identify the page, like a subject. */
  readonly query = input<Params | null>(null);
  protected readonly tabs = computed(() => [
    { key: 'details' as const, label: 'Details', link: this.base() },
    { key: 'mcp' as const, label: 'MCP', link: [...this.base(), 'mcp'] },
  ]);
}
