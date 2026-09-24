import { CatalogToolSelection, SelectionExplanation } from '../../api/models';

export type Filter = 'all' | 'exposed' | 'left-out';

/** Why a catalog tool is or is not exposed, in a sentence. */
export function reason(t: CatalogToolSelection): string {
  const list = (xs: string[]) => xs.join(', ');
  switch (t.state) {
    case 'selected':
      return `selected by ${list(t.selectedBy)}${t.renamedBy ? `, renamed by ${t.renamedBy}` : ''}`;
    case 'excluded':
      return `matched ${list(t.selectedBy)}, removed by ${list(t.excludedBy)}`;
    case 'conflict':
      return `selected by ${list(t.selectedBy)}, but another tool of this set is exposed as ${t.exposedAs} too, so neither is`;
    default:
      return 'no entry of tools matches it';
  }
}

/** Whether a row belongs to what an entry of spec (or an alias) decided. */
export function touchedBy(t: CatalogToolSelection, focus: string | null): boolean {
  if (!focus) {
    return false;
  }
  if (focus.startsWith('alias:')) {
    return t.tool.name === focus.slice('alias:'.length);
  }
  return t.selectedBy.includes(focus) || t.excludedBy.includes(focus);
}

export function filtered(sel: SelectionExplanation, filter: Filter): CatalogToolSelection[] {
  switch (filter) {
    case 'exposed':
      return sel.tools.filter((t) => t.state === 'selected');
    case 'left-out':
      return sel.tools.filter((t) => t.state !== 'selected');
    default:
      return sel.tools;
  }
}

/**
 * Names the controller reports and this replica does not, or the other way
 * round. Both resolve with the same code, so a difference means they hold
 * different catalogs — one of them has not seen the latest yet.
 */
export function disagreement(statusTools: string[] | undefined, sel: SelectionExplanation): string[] {
  const here = new Set(sel.tools.filter((t) => t.state === 'selected').map((t) => t.exposedAs ?? t.tool.name));
  const there = new Set(statusTools ?? []);
  return [...[...here].filter((n) => !there.has(n)), ...[...there].filter((n) => !here.has(n))].sort();
}
