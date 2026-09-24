import { computed, inject, Injectable } from '@angular/core';
import { listCapabilities, listGrants, listServers, listToolSets } from '../api/functions';
import { CapabilitySummary, GrantSummary, ServerSummary, ToolSetSummary } from '../api/models';
import { Inspect, live } from './inspect';

/**
 * The four lists, loaded once for the whole page and refreshed with it: the
 * navigation counts them, the overview draws them, and detail pages look up
 * the state of what they link to.
 */
@Injectable({ providedIn: 'root' })
export class Lists {
  private readonly inspect = inject(Inspect);

  readonly servers = live(() => ({}), () => this.inspect.call(listServers, {}));
  readonly toolSets = live(() => ({}), () => this.inspect.call(listToolSets, {}));
  readonly capabilities = live(() => ({}), () => this.inspect.call(listCapabilities, {}));
  readonly grants = live(() => ({}), () => this.inspect.call(listGrants, {}));

  readonly serverItems = computed<ServerSummary[]>(() => (this.servers.hasValue() ? (this.servers.value()?.items ?? []) : []));
  readonly toolSetItems = computed<ToolSetSummary[]>(() => (this.toolSets.hasValue() ? (this.toolSets.value()?.items ?? []) : []));
  readonly capabilityItems = computed<CapabilitySummary[]>(() =>
    this.capabilities.hasValue() ? (this.capabilities.value()?.items ?? []) : [],
  );
  readonly grantItems = computed<GrantSummary[]>(() => (this.grants.hasValue() ? (this.grants.value()?.items ?? []) : []));

  readonly loading = computed(
    () => this.servers.isLoading() || this.toolSets.isLoading() || this.capabilities.isLoading() || this.grants.isLoading(),
  );
  readonly error = computed(() => this.servers.error() ?? this.toolSets.error() ?? this.capabilities.error() ?? this.grants.error());
  readonly loaded = computed(
    () => this.servers.hasValue() && this.toolSets.hasValue() && this.capabilities.hasValue() && this.grants.hasValue(),
  );

  toolSet(name: string): ToolSetSummary | undefined {
    return this.toolSetItems().find((t) => t.meta.name === name);
  }

  capability(name: string): CapabilitySummary | undefined {
    return this.capabilityItems().find((c) => c.meta.name === name);
  }

  server(name: string): ServerSummary | undefined {
    return this.serverItems().find((s) => s.meta.name === name);
  }
}
