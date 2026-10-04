import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { MpTip } from '@metapatrol/ng';
import { ToolAnnotations, ToolDefinition } from '../api/models';
import { parameters } from '../core/schema';

/**
 * A tool's description and arguments: the text an agent decides by. It is
 * shown as the backend sent it; the gateway passes it on unchanged.
 */
@Component({
  selector: 'app-tool-doc',
  imports: [MpTip],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (tool().description; as d) {
      <p class="mt-1 max-w-[72ch] font-sans text-[12.5px] leading-snug whitespace-pre-line text-fg-muted">{{ d }}</p>
    } @else {
      <p class="text-fg-subtle mt-1 font-sans text-[12.5px]">No description.</p>
    }
    <div class="mt-1 flex flex-wrap items-center gap-1 font-sans text-[10.5px]">
      @if (hints().length) {
        <span class="text-fg-subtle">declared by the backend:</span>
        @for (h of hints(); track h.text) {
          <span class="rounded px-1.5 py-0.5 font-medium" [class]="h.classes" [mpTip]="h.meaning + '\nDeclared by the backend, not verified by the gateway.'" [focusable]="true">{{ h.text }}</span>
        }
      } @else {
        <span class="text-fg-subtle">no hints declared</span>
      }
    </div>
    @if (params().length) {
      <details class="mt-1 font-sans text-xs" [open]="open()">
        <summary class="w-max cursor-pointer text-signal-fg">
          input · {{ params().length }} {{ params().length === 1 ? 'parameter' : 'parameters' }}
        </summary>
        <ul class="mt-1.5 flex flex-col gap-1 border-l-2 border-line pl-2.5">
          @for (p of params(); track p.name) {
            <li class="flex flex-wrap items-baseline gap-x-2">
              <code class="text-fg">{{ p.name }}</code>
              <span class="font-mono text-[11.5px] text-info">{{ p.type }}</span>
              @if (p.required) {
                <span class="text-[10px] font-semibold tracking-wide text-warning uppercase">required</span>
              }
              @if (p.description) {
                <span class="text-fg-subtle">{{ p.description }}</span>
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
  protected readonly hints = computed(() => declaredHints(this.tool().annotations));
}

interface Hint {
  text: string;
  classes: string;
  /** What the annotation means, in MCP's words. */
  meaning: string;
}

const MEANING: Record<string, string> = {
  'read-only': 'Does not modify its environment.',
  destructive: 'May perform destructive updates to its environment.',
  'not destructive': 'Performs only additive updates to its environment.',
  idempotent: 'Calling it again with the same arguments has no additional effect.',
  'open world': 'Interacts with an open world of external entities, such as the web.',
  'closed world': 'Its domain of interaction is closed.',
};

const GOOD = 'bg-success/12 text-success';
const BAD = 'bg-error/12 text-error';
const PLAIN = 'bg-surface-2 text-fg-muted';

/**
 * The hints a backend actually declared, and nothing it did not. MCP's
 * defaults — a tool that says nothing may destroy data and reach outside —
 * are not shown as if the backend had said so.
 *
 * readOnlyHint and idempotentHint cannot be told apart from undeclared when
 * false (the SDK drops the difference), so only true counts for them.
 * destructiveHint means something only for a tool that is not read-only.
 */
export function declaredHints(a: ToolAnnotations | undefined): Hint[] {
  if (!a) {
    return [];
  }
  const out: Hint[] = [];
  if (a.readOnlyHint) {
    out.push({ text: 'read-only', classes: GOOD, meaning: MEANING['read-only'] });
  } else if (a.destructiveHint !== undefined && a.destructiveHint !== null) {
    out.push(
      a.destructiveHint
        ? { text: 'destructive', classes: BAD, meaning: MEANING['destructive'] }
        : { text: 'not destructive', classes: PLAIN, meaning: MEANING['not destructive'] },
    );
  }
  if (a.idempotentHint) {
    out.push({ text: 'idempotent', classes: PLAIN, meaning: MEANING['idempotent'] });
  }
  if (a.openWorldHint !== undefined && a.openWorldHint !== null) {
    const text = a.openWorldHint ? 'open world' : 'closed world';
    out.push({ text, classes: PLAIN, meaning: MEANING[text] });
  }
  return out;
}
