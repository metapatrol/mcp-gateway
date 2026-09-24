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
import { ApiConfiguration } from '../api/api-configuration';
import { watchEvents } from '../api/functions';
import { Source } from '../api/models';
import { StrictHttpResponse } from '../api/strict-http-response';

/** A generated API function: every answer of the inspect API carries its source. */
type Operation<P, R> = (
  http: HttpClient,
  rootUrl: string,
  params: P,
  context?: HttpContext,
) => Observable<StrictHttpResponse<R>>;

/** Polling without the event stream. */
const POLL_MS = 30_000;
/**
 * Polling with it: a safety net for what no event announces, such as a
 * catalog ageing out of use while its backend stays down.
 */
const LIVE_POLL_MS = 5 * 60_000;
/** How long to wait before opening a stream the gateway refused. */
const REOPEN_MS = 30_000;

export type Liveness = 'connecting' | 'live' | 'reconnecting' | 'polling';

/**
 * Inspect calls the gateway's inspect API through the generated client and
 * keeps what the whole page shares: when to read again.
 *
 * When to read again is the gateway's event stream: each `changed` event
 * reloads what the page shows. The stream only says that something changed;
 * the data still comes through the other operations. Without the stream —
 * the browser has no EventSource, or the gateway refuses one — the page
 * polls.
 */
@Injectable({ providedIn: 'root' })
export class Inspect {
  private readonly api = inject(Api);
  private readonly config = inject(ApiConfiguration);
  private readonly document = inject(DOCUMENT);

  /** Bumped by every refresh, manual or automatic. */
  readonly refreshes = signal(0);
  /** Whether updates arrive as they happen, or by polling. */
  readonly liveness = signal<Liveness>('connecting');

  private stream?: EventSource;
  private lastRefresh = Date.now();

  constructor() {
    const destroy = inject(DestroyRef);
    const timer = setInterval(() => {
      const every = this.liveness() === 'live' ? LIVE_POLL_MS : POLL_MS;
      if (this.document.visibilityState !== 'hidden' && Date.now() - this.lastRefresh >= every) {
        this.refresh();
      }
    }, 5_000);
    destroy.onDestroy(() => {
      clearInterval(timer);
      this.stream?.close();
    });
    this.open();
  }

  async call<P, R extends { source: Source }>(op: Operation<P, R>, params: P): Promise<R> {
    return this.api.invoke(op as Operation<P, R>, params);
  }

  refresh(): void {
    this.lastRefresh = Date.now();
    this.refreshes.update((n) => n + 1);
  }

  private open(): void {
    const view = this.document.defaultView as (Window & typeof globalThis) | null;
    if (!view?.EventSource) {
      this.liveness.set('polling');
      return;
    }
    const stream = new view.EventSource(`${this.config.rootUrl}${watchEvents.PATH}`);
    this.stream = stream;
    let lost = false;
    stream.onopen = () => {
      // Whatever happened while the stream was down was not announced.
      if (lost) {
        this.refresh();
      }
      lost = false;
      this.liveness.set('live');
    };
    stream.addEventListener('changed', () => this.refresh());
    stream.onerror = () => {
      lost = true;
      if (stream.readyState === EventSource.CLOSED) {
        // Refused outright — 401, 503 — which EventSource does not retry.
        // Poll, and ask again later.
        this.liveness.set('polling');
        setTimeout(() => this.open(), REOPEN_MS);
      } else {
        this.liveness.set('reconnecting');
      }
    };
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

