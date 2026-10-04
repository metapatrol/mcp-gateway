import { conditionTone, ConditionTone, findCondition } from '@metapatrol/ng';
import { Condition } from '../api/models';

export { firstProblem, healthy } from '@metapatrol/ng';

export type Tone = ConditionTone;

/**
 * Reasons that only pass on a dependency's trouble: the resource itself is
 * fine, something it names is not. They are shown as warnings, so that the
 * one resource actually at fault stands out. The console's condition pills
 * read them from provideMetapatrol (app.config.ts).
 */
export const INHERITED = ['ServerUnavailable', 'ToolSetNotReady', 'CapabilityNotReady', 'Invalid'];

export const tone = (c: Condition | undefined): Tone => conditionTone(c, INHERITED);

export function condition(
  conditions: Condition[] | undefined,
  type: Condition['type'],
): Condition | undefined {
  return findCondition(conditions, type);
}

export const SERVER_CONDITIONS: Condition['type'][] = ['Ready'];
export const CONDITIONS: Condition['type'][] = ['Valid', 'Ready'];
