import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { ToolAnnotations, ToolDefinition } from '../api/models';
import { parameters } from '../core/schema';

/**
 * A tool's description and arguments: the text an agent decides by. It is
 * shown as the backend sent it; the gateway passes it on unchanged.
 */
@Component({
  selector: 'app-tool-doc',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (tool().description; as d) {
      <p class="mt-1 max-w-[72ch] font-sans text-[12.5px] leading-snug whitespace-pre-line text-slate-600 dark:text-slate-400">{{ d }}</p>
    } @else {
      <p class="faint mt-1 font-sans text-[12.5px]">No description.</p>
    }
    <div class="mt-1 flex flex-wrap items-center gap-1 font-sans text-[10.5px]" [title]="HINT_TITLE">
      @if (hints().length) {
        <span class="faint">declared by the backend:</span>
        @for (h of hints(); track h.text) {
          <span class="rounded px-1.5 py-0.5 font-medium" [class]="h.classes">{{ h.text }}</span>
        }
      } @else {
        <span class="faint">no hints declared</span>
      }
    </div>
    @if (params().length) {
      <details class="mt-1 font-sans text-xs" [open]="open()">
        <summary class="w-max cursor-pointer text-accent-600 dark:text-accent-300">
          input · {{ params().length }} {{ params().length === 1 ? 'parameter' : 'parameters' }}
        </summary>
        <ul class="mt-1.5 flex flex-col gap-1 border-l-2 border-slate-200 pl-2.5 dark:border-slate-700">
          @for (p of params(); track p.name) {
            <li class="flex flex-wrap items-baseline gap-x-2">
              <code class="text-slate-900 dark:text-slate-100">{{ p.name }}</code>
              <span class="font-mono text-[11.5px] text-teal-700 dark:text-teal-300">{{ p.type }}</span>
              @if (p.required) {
                <span class="text-[10px] font-semibold tracking-wide text-amber-700 uppercase dark:text-amber-400">required</span>
              }
              @if (p.description) {
                <span class="faint">{{ p.description }}</span>
              }
            </li>
          }
        </ul>
      </details>
    }
  `,
})
export class ToolDoc {
  readonly tool = input.required<ToolDefinition>();
  readonly open = input(false);
  protected readonly params = computed(() => parameters(this.tool().inputSchema));
  protected readonly HINT_TITLE =
    "The backend's own claims about this tool. The gateway verifies none of them. MCP assumes a tool that declares nothing may modify and destroy data, and reaches outside.";
  protected readonly hints = computed(() => declaredHints(this.tool().annotations));
}

const GOOD = 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/60 dark:text-emerald-300';
const BAD = 'bg-red-50 text-red-700 dark:bg-red-950/60 dark:text-red-300';
const PLAIN = 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300';

/**
 * The hints a backend actually declared, and nothing it did not. MCP's
 * defaults — a tool that says nothing may destroy data and reach outside —
 * are not shown as if the backend had said so.
 *
 * readOnlyHint and idempotentHint cannot be told apart from undeclared when
 * false (the SDK drops the difference), so only true counts for them.
 * destructiveHint means something only for a tool that is not read-only.
 */
export function declaredHints(a: ToolAnnotations | undefined): { text: string; classes: string }[] {
  if (!a) {
    return [];
  }
  const out: { text: string; classes: string }[] = [];
  if (a.readOnlyHint) {
    out.push({ text: 'read-only', classes: GOOD });
  } else if (a.destructiveHint !== undefined && a.destructiveHint !== null) {
    out.push(a.destructiveHint ? { text: 'destructive', classes: BAD } : { text: 'not destructive', classes: PLAIN });
  }
  if (a.idempotentHint) {
    out.push({ text: 'idempotent', classes: PLAIN });
  }
  if (a.openWorldHint !== undefined && a.openWorldHint !== null) {
    out.push({ text: a.openWorldHint ? 'open world' : 'closed world', classes: PLAIN });
  }
  return out;
}
