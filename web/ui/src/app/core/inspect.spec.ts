import { parseBatch } from './inspect';

describe('parseBatch', () => {
  it('reads the data of a changed event', () => {
    expect(parseBatch('{"changes":[{"kind":"toolset","name":"web-search"}]}')).toEqual({
      changes: [{ kind: 'toolset', name: 'web-search' }],
    });
  });

  it('ignores what is not a batch', () => {
    expect(parseBatch('not json')).toBeUndefined();
    expect(parseBatch('{"something":"else"}')).toBeUndefined();
    expect(parseBatch('null')).toBeUndefined();
  });
});
