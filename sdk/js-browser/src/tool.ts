/**
 * `tool()` helper — builds a {@link ToolDefinition} the {@link Agent} can use.
 *
 * Identical surface to the Node SDK: callers must pass a JSON Schema for the
 * tool parameters explicitly. The handler receives the parsed `args` object
 * (typed via the generic `P`) and returns either a string, a structured
 * `ToolExecuteResult`, or any value that will be coerced to a string.
 */

export interface ToolExecuteResult {
  content: string;
  is_error?: boolean;
  metadata?: Record<string, unknown>;
}

export type ToolReturn =
  | string
  | ToolExecuteResult
  | number
  | boolean
  | null
  | undefined;

export type ToolHandler<P = Record<string, unknown>> = (
  args: P,
) => ToolReturn | Promise<ToolReturn>;

export interface ToolDefinition<P = Record<string, unknown>> {
  readonly name: string;
  readonly description: string;
  readonly parameters: Record<string, unknown>;
  readonly readOnly: boolean;
  readonly handler: ToolHandler<P>;
}

export interface ToolOptions<P = Record<string, unknown>> {
  name: string;
  description: string;
  parameters: Record<string, unknown>;
  readOnly?: boolean;
  handler: ToolHandler<P>;
}

export function tool<P = Record<string, unknown>>(
  opts: ToolOptions<P>,
): ToolDefinition<P> {
  if (!opts.name) throw new TypeError('tool({ name }) is required');
  if (typeof opts.handler !== 'function') {
    throw new TypeError('tool({ handler }) must be a function');
  }
  return {
    name: opts.name,
    description: opts.description ?? '',
    parameters: opts.parameters ?? { type: 'object' },
    readOnly: opts.readOnly ?? false,
    handler: opts.handler,
  };
}

/** Normalise a tool handler return value into the structured shape. */
export function coerceToolResult(raw: ToolReturn): ToolExecuteResult {
  if (raw === null || raw === undefined) {
    return { content: '', is_error: false };
  }
  if (typeof raw === 'string') {
    return { content: raw, is_error: false };
  }
  if (typeof raw === 'object' && 'content' in raw) {
    const out: ToolExecuteResult = { content: String(raw.content ?? '') };
    if (typeof raw.is_error === 'boolean') out.is_error = raw.is_error;
    if (raw.metadata && typeof raw.metadata === 'object') out.metadata = raw.metadata;
    return out;
  }
  return { content: String(raw), is_error: false };
}

/**
 * Render a list of tool definitions as a markdown chunk that a small LLM can
 * read inside the router system prompt. Mirrors `Registry.formatDefs` in the
 * Go core.
 */
export function formatToolsForPrompt(defs: ReadonlyArray<ToolDefinition>): string {
  if (defs.length === 0) return '';
  let out = '## Available Tools\n\n';
  for (const def of defs) {
    out += `### ${def.name}\n`;
    out += `${def.description}\n`;
    out += 'Parameters:\n```json\n';
    try {
      out += JSON.stringify(def.parameters, null, 2);
    } catch {
      out += '{}';
    }
    out += '\n```\n\n';
  }
  return out;
}
