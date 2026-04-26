/**
 * Compose system prompts for the chat step and the router step.
 *
 * The structure mirrors `internal/engine/router.go`:
 *
 *   <system_prompt>
 *
 *   ## Available Tools
 *   ### tool_a
 *   ...
 *
 *   ## Instructions
 *   ... JSON-mode instructions ...
 *
 * The chat-step variant skips the tools / instructions sections — at that
 * point the router has already chosen `none` and we just want a normal reply.
 */

import { type ToolDefinition, formatToolsForPrompt } from '../tool.js';

const ROUTER_INSTRUCTIONS = `## Instructions

Based on the user's request and conversation history, select the most appropriate tool to use.

Select tool "none" when:
- You already have enough information to answer (e.g., tool results are already in the conversation)
- The question can be answered directly without any tool
- You need to summarize, explain, or respond based on previous tool results

IMPORTANT: Do NOT use a tool to deliver your answer. Tools are for gathering information only. When you have the information needed, select "none" and the system will generate the response.

You MUST respond with a JSON object in this exact format:
{"tool": "<tool_name or none>", "arguments": {<tool arguments>}, "reasoning": "<brief explanation>"}

Respond with valid JSON only.
`;

export interface PromptOptions {
  systemPrompt?: string;
  tools: ReadonlyArray<ToolDefinition>;
}

/** Router system prompt — includes tools + JSON-mode instructions. */
export function buildRouterSystemPrompt(opts: PromptOptions): string {
  const parts: string[] = [];
  if (opts.systemPrompt && opts.systemPrompt.trim()) {
    parts.push(opts.systemPrompt.trim());
  }
  const toolsSection = formatToolsForPrompt(opts.tools);
  if (toolsSection) parts.push(toolsSection);
  parts.push(ROUTER_INSTRUCTIONS);
  return parts.join('\n\n');
}

/** Chat-step system prompt — only the user-supplied prompt (or empty). */
export function buildChatSystemPrompt(opts: PromptOptions): string {
  return (opts.systemPrompt ?? '').trim();
}
