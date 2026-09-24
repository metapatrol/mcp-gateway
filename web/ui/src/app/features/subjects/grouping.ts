import { SubjectTool } from '../../api/models';

export interface ServerGroup {
  server: string;
  tools: SubjectTool[];
}

/**
 * Offered tools by the server they come from, servers and tools sorted —
 * the way the instructions an agent is sent are arranged too.
 */
export function byServer(tools: SubjectTool[]): ServerGroup[] {
  const groups = new Map<string, SubjectTool[]>();
  for (const t of tools) {
    if (t.state !== 'offered') {
      continue;
    }
    const server = t.claimants[0]?.ref.server ?? '';
    groups.set(server, [...(groups.get(server) ?? []), t]);
  }
  return [...groups.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([server, list]) => ({ server, tools: list.sort((a, b) => a.name.localeCompare(b.name)) }));
}

/** The tools resolved for the subject that it is not offered. */
export function notOffered(tools: SubjectTool[]): SubjectTool[] {
  return tools.filter((t) => t.state !== 'offered');
}
