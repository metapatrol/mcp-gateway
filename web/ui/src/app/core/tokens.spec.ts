import { AgentExchange, BackendExchange } from '../api/models';
import { contextCost, heaviest, toolText } from './tokens';

// One token per character keeps the arithmetic visible.
const count = (text: string) => text.length;

const tool = (name: string, description: string) => ({
  name,
  description,
  inputSchema: { type: 'object' },
  annotations: { readOnlyHint: true },
});

describe('contextCost', () => {
  it('counts what the model is given of an agent exchange: tools and instructions, not the envelope', () => {
    const a = tool('a', 'Does a.');
    const b = tool('b', 'Does b, at length.');
    const exchange = {
      discover: { jsonrpc: '2.0', id: 1, result: { cacheScope: 'private', instructions: 'Use a first.' } },
      toolsList: [
        { jsonrpc: '2.0', id: 1, result: { cacheScope: 'private', tools: [a] } },
        { jsonrpc: '2.0', id: 2, result: { tools: [b] } },
      ],
    } as unknown as AgentExchange;

    const cost = contextCost(exchange, count);
    expect(cost.pages.map((p) => p.tokens)).toEqual([toolText(a).length, toolText(b).length]);
    expect(cost.instructions).toBe('Use a first.'.length);
    expect(cost.total).toBe(toolText(a).length + toolText(b).length + 'Use a first.'.length);
  });

  it('reads a backend exchange, whose pages are bare results', () => {
    const exchange = {
      handshake: { instructions: 'Hi.', serverInfo: { name: 'x' } },
      toolsList: [{ tools: [tool('a', 'A.')] }],
    } as unknown as BackendExchange;
    const cost = contextCost(exchange, count);
    expect(cost.pages[0].tools[0].name).toBe('a');
    expect(cost.instructions).toBe(3);
  });

  it('leaves annotations out of a tool', () => {
    expect(toolText(tool('a', 'A.'))).not.toContain('readOnlyHint');
    expect(toolText(tool('a', 'A.'))).toContain('input_schema');
  });
});

describe('heaviest', () => {
  it('lists the most expensive tools first', () => {
    expect(heaviest([{ name: 'a', tokens: 5 }, { name: 'b', tokens: 1200 }, { name: 'c', tokens: 7 }], 2)).toBe('b  1,200\nc  7');
  });
});
