import { Condition } from '../api/models';
import { CONDITIONS, firstProblem, healthy, tone } from './conditions';

const c = (type: Condition['type'], status: Condition['status'], reason: string): Condition => ({
  type,
  status,
  reason,
  message: '',
  lastTransitionTime: '2026-09-24T00:00:00Z',
});

describe('conditions', () => {
  it('tells a resource at fault from one passing trouble on', () => {
    expect(tone(c('Ready', 'True', 'Available'))).toBe('ok');
    expect(tone(c('Ready', 'False', 'MissingTools'))).toBe('bad');
    expect(tone(c('Ready', 'False', 'ServerUnavailable'))).toBe('warn');
    expect(tone(undefined)).toBe('neutral');
  });

  it('is healthy only with every condition True', () => {
    expect(healthy([c('Valid', 'True', 'Resolved'), c('Ready', 'True', 'Available')], CONDITIONS)).toBe(true);
    expect(healthy([c('Valid', 'True', 'Resolved')], CONDITIONS)).toBe(false);
    expect(healthy([], CONDITIONS)).toBe(false);
  });

  it('names the first problem, Valid before Ready', () => {
    const conds = [c('Ready', 'False', 'Invalid'), c('Valid', 'False', 'ToolNameConflict')];
    expect(firstProblem(conds, CONDITIONS)?.reason).toBe('ToolNameConflict');
  });
});
