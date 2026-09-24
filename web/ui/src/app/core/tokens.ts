import { AgentExchange, BackendExchange } from '../api/models';

/**
 * What a payload costs in a model's context, estimated.
 *
 * Not the JSON-RPC as sent: an MCP client hands the model each tool's name,
 * description and input schema, and puts the instructions into the system
 * prompt. The envelope, server info, cache hints and annotations stay with
 * the client. Those parts are what is counted here.
 *
 * The count is o200k_base, an open BPE vocabulary. Claude's tokenizer is not
 * published, so the numbers are an estimate — close enough to see which tool
 * set is heavy and which tools make it so, not to budget to the token.
 */

export type Count = (text: string) => number;

export interface ToolCost {
  name: string;
  tokens: number;
}

export interface PageCost {
  tools: ToolCost[];
  tokens: number;
}

export interface ContextCost {
  /** Per tools/list page, in order. */
  pages: PageCost[];
  instructions: number;
  total: number;
}

export const TOKENIZER = 'o200k_base';

let loading: Promise<Count> | undefined;

/** The tokenizer, loaded on first use: its vocabulary is a couple of MB. */
export function tokenCounter(): Promise<Count> {
  loading ??= import('gpt-tokenizer/encoding/o200k_base').then((m) => (text: string) => m.countTokens(text));
  return loading;
}

interface WireTool {
  name?: string;
  description?: string;
  inputSchema?: unknown;
}

/** The part of a tool the model is given. */
export function toolText(tool: WireTool): string {
  return JSON.stringify({ name: tool.name, description: tool.description ?? '', input_schema: tool.inputSchema ?? {} });
}

/** An agent's pages are JSON-RPC responses; a backend's are the results as read. */
function toolsOf(page: unknown): WireTool[] {
  const p = page as { result?: { tools?: WireTool[] }; tools?: WireTool[] } | null;
  return p?.result?.tools ?? p?.tools ?? [];
}

function instructionsOf(exchange: AgentExchange | BackendExchange): string {
  if ('discover' in exchange) {
    return (exchange.discover as { result?: { instructions?: string } } | null)?.result?.instructions ?? '';
  }
  return (exchange.handshake as { instructions?: string } | null | undefined)?.instructions ?? '';
}

export function contextCost(exchange: AgentExchange | BackendExchange, count: Count): ContextCost {
  const pages = exchange.toolsList.map((page) => {
    const tools = toolsOf(page).map((t) => ({ name: t.name ?? '', tokens: count(toolText(t)) }));
    return { tools, tokens: tools.reduce((n, t) => n + t.tokens, 0) };
  });
  const text = instructionsOf(exchange);
  const instructions = text ? count(text) : 0;
  return { pages, instructions, total: pages.reduce((n, p) => n + p.tokens, 0) + instructions };
}

/** The heaviest tools, for a tooltip. */
export function heaviest(tools: ToolCost[], n = 5): string {
  return [...tools]
    .sort((a, b) => b.tokens - a.tokens || a.name.localeCompare(b.name))
    .slice(0, n)
    .map((t) => `${t.name}  ${t.tokens.toLocaleString('en')}`)
    .join('\n');
}
