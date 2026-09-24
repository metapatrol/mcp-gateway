import { Condition } from '../api/models';

export type Tone = 'ok' | 'bad' | 'warn' | 'neutral';

export function condition(conditions: Condition[] | undefined, type: Condition['type']): Condition | undefined {
  return conditions?.find((c) => c.type === type);
}

/**
 * Reasons that only pass on a dependency's trouble: the resource itself is
 * fine, something it names is not. They are shown as warnings, so that the
 * one resource actually at fault stands out.
 */
const inherited = new Set(['ServerUnavailable', 'ToolSetNotReady', 'CapabilityNotReady', 'Invalid']);

export function tone(c: Condition | undefined): Tone {
  if (!c || c.status === 'Unknown') {
    return 'neutral';
  }
  if (c.status === 'True') {
    return 'ok';
  }
  return inherited.has(c.reason) ? 'warn' : 'bad';
}

/**
 * Healthy is every condition the kind carries being True: Ready for a
 * server, Valid and Ready for the rest. A resource the controller has not
 * reported on yet is not healthy.
 */
export function healthy(conditions: Condition[] | undefined, types: Condition['type'][]): boolean {
  return types.every((t) => condition(conditions, t)?.status === 'True');
}

/** The first condition that is not True, the one worth showing. */
export function firstProblem(conditions: Condition[] | undefined, types: Condition['type'][]): Condition | undefined {
  for (const t of types) {
    const c = condition(conditions, t);
    if (c && c.status !== 'True') {
      return c;
    }
  }
  return undefined;
}

export const SERVER_CONDITIONS: Condition['type'][] = ['Ready'];
export const CONDITIONS: Condition['type'][] = ['Valid', 'Ready'];
