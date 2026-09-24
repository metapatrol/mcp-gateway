import { HttpClient, HttpContext, HttpErrorResponse } from '@angular/common/http';
import {
  DestroyRef,
  effect,
  inject,
  Injectable,
  resource,
  ResourceRef,
  signal,
  untracked,
} from '@angular/core';
import { DOCUMENT } from '@angular/common';
import { Observable } from 'rxjs';
import { Api } from '../api/api';
import { Source } from '../api/models';
import { StrictHttpResponse } from '../api/strict-http-response';

/** A generated API function: every answer of the inspect API carries its source. */
type Operation<P, R> = (
  http: HttpClient,
  rootUrl: string,
  params: P,
  context?: HttpContext,
) => Observable<StrictHttpResponse<R>>;

const SOURCES_KEY = 'mcp-gateway-ui.show-sources';
const AUTO_REFRESH_MS = 30_000;

/**
 * Inspect calls the gateway's inspect API through the generated client and
 * keeps what the whole page shares: which replica answered last, when to
 * refresh, and whether data sources are marked.
 */
@Injectable({ providedIn: 'root' })
export class Inspect {
  private readonly api = inject(Api);
  private readonly document = inject(DOCUMENT);

  /** The replica and time of the most recent answer. */
  readonly source = signal<Source | undefined>(undefined);
  /** Bumped by every refresh, manual or automatic. */
  readonly refreshes = signal(0);
  readonly showSources = signal(this.readShowSources());

  constructor() {
    // Every 30 seconds while the page is visible: the data is live, and a
    // console left open should not show last hour's catalog.
    const timer = setInterval(() => {
      if (this.document.visibilityState !== 'hidden') {
        this.refresh();
      }
    }, AUTO_REFRESH_MS);
    inject(DestroyRef).onDestroy(() => clearInterval(timer));
  }

  async call<P, R extends { source: Source }>(op: Operation<P, R>, params: P): Promise<R> {
    const answer = await this.api.invoke(op as Operation<P, R>, params);
    this.source.set(answer.source);
    return answer;
  }

  refresh(): void {
    this.refreshes.update((n) => n + 1);
  }

  toggleSources(): void {
    const next = !this.showSources();
    this.showSources.set(next);
    try {
      this.document.defaultView?.localStorage.setItem(SOURCES_KEY, String(next));
    } catch {
      // Without storage the choice lasts for this page.
    }
  }

  private readShowSources(): boolean {
    try {
      return this.document.defaultView?.localStorage.getItem(SOURCES_KEY) !== 'false';
    } catch {
      return true;
    }
  }
}

/**
 * A resource over the inspect API that reloads on every refresh, keeping its
 * value while it does. Call it in an injection context.
 */
export function live<P, R>(params: () => P, load: (params: NoInfer<Exclude<P, undefined>>) => Promise<R>): ResourceRef<R | undefined> {
  const inspect = inject(Inspect);
  const ref = resource({ params, loader: ({ params }) => load(params) });
  let first = true;
  effect(() => {
    inspect.refreshes();
    if (first) {
      first = false;
      return;
    }
    untracked(() => ref.reload());
  });
  return ref;
}

/** A message for an error from the API, preferring the problem's detail. */
export function describeError(err: unknown): string {
  if (err instanceof HttpErrorResponse) {
    const problem = err.error as { detail?: string; title?: string } | null;
    if (problem && typeof problem === 'object' && (problem.detail || problem.title)) {
      return `${err.status} ${problem.title ?? ''}: ${problem.detail ?? ''}`.trim();
    }
    if (err.status === 0) {
      return 'The console could not be reached.';
    }
    return `${err.status} ${err.statusText}`;
  }
  return err instanceof Error ? err.message : String(err);
}

/** Whether an error is the API saying the resource does not exist. */
export function isNotFound(err: unknown): boolean {
  return err instanceof HttpErrorResponse && err.status === 404;
}
