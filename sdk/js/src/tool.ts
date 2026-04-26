/**
 * `tool()` helper — builds a {@link ToolDefinition} that the {@link Agent}
 * can register with the Go core.
 *
 * Unlike Python, TypeScript has no run-time type info, so callers must pass
 * a JSON Schema for the tool parameters explicitly. The handler receives the
 * parsed `args` object (typed via the generic `P` parameter) and returns
 * either a string, a structured `ToolExecuteResult`, or any value that will
 * be coerced to a string.
 *
 * @example
 * ```ts
 * const readFile = tool({
 *   name: 'read_file',
 *   description: 'Read a UTF-8 text file from the workspace.',
 *   parameters: {
 *     type: 'object',
 *     properties: { path: { type: 'string' } },
 *     required: ['path'],
 *     additionalProperties: false,
 *   },
 *   readOnly: true,
 *   handler: async ({ path }) => await readFile(path, 'utf8'),
 * });
 * ```
 */

/** Shape of the result the core expects (matches `ToolExecuteResult.json`). */
export interface ToolExecuteResult {
  content: string;
  is_error?: boolean;
  metadata?: Record<string, unknown>;
}

/** What a {@link ToolHandler} may return; we coerce non-strings to strings. */
export type ToolReturn = string | ToolExecuteResult | number | boolean | null | undefined;

export type ToolHandler<P = Record<string, unknown>> = (
  args: P,
) => ToolReturn | Promise<ToolReturn>;

export interface ToolDefinition<P = Record<string, unknown>> {
  /** Tool name exposed to the agent. */
  readonly name: string;
  /** Human-readable description (shown to the model). */
  readonly description: string;
  /** JSON Schema (`type: object`) describing the `args`. */
  readonly parameters: Record<string, unknown>;
  /** True when the tool has no observable side-effects (auto-approval). */
  readonly readOnly: boolean;
  /** Wrapper-side handler invoked from `tool.execute`. */
  readonly handler: ToolHandler<P>;
}

export interface ToolOptions<P = Record<string, unknown>> {
  name: string;
  description: string;
  parameters: Record<string, unknown>;
  readOnly?: boolean;
  handler: ToolHandler<P>;
}

/** Construct a {@link ToolDefinition}. */
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

/** Internal: project a {@link ToolDefinition} into the wire format. */
export function toolToProtocolDict(def: ToolDefinition): Record<string, unknown> {
  return {
    name: def.name,
    description: def.description,
    parameters: def.parameters,
    read_only: def.readOnly,
  };
}

/** Internal: normalise a {@link ToolHandler} return value. */
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
