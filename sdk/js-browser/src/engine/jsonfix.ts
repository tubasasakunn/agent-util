/**
 * Repair small-LLM JSON output.
 *
 * Direct TypeScript port of `internal/llm/fixjson.go` — the same regex-free
 * passes, applied in the same order:
 *
 *   1. Strip leading ```json ... ``` code fences
 *   2. `{null}` -> `null`
 *   3. Strip control chars inside strings (except \t \n \r)
 *   4. Single quotes -> double quotes (only if no double quotes already)
 *   5. Drop trailing commas before `}` / `]`
 *   6. Append missing closing `}` / `]`
 *   7. Merge concatenated `{...}\n{...}` objects into one object
 *
 * Always returns a string. If repair leaves the JSON still invalid, the
 * caller is expected to handle the parse error.
 */

export function fixJson(data: string): string {
  let r = data;
  r = stripCodeBlock(r);
  r = fixNullBraces(r);
  r = fixControlChars(r);
  r = fixSingleQuotes(r);
  r = fixTrailingCommas(r);
  r = fixUnmatchedBrackets(r);
  r = mergeJsonObjects(r);
  return r;
}

/** Strip a leading ```...``` (optional language tag) fence. */
export function stripCodeBlock(data: string): string {
  const trimmed = data.trim();
  if (!trimmed.startsWith('```')) return data;
  const firstNl = trimmed.indexOf('\n');
  if (firstNl < 0) return data;
  let inner = trimmed.slice(firstNl + 1);
  const closeIdx = inner.indexOf('```');
  if (closeIdx >= 0) inner = inner.slice(0, closeIdx);
  return inner.trim();
}

export function fixNullBraces(data: string): string {
  return data.split('{null}').join('null');
}

/** Drop ASCII control characters inside JSON strings, preserving \t \n \r. */
export function fixControlChars(data: string): string {
  let out = '';
  let inString = false;
  let escaped = false;
  for (let i = 0; i < data.length; i++) {
    const ch = data.charCodeAt(i);
    if (escaped) {
      out += data[i];
      escaped = false;
      continue;
    }
    if (ch === 0x5c /* \\ */ && inString) {
      out += data[i];
      escaped = true;
      continue;
    }
    if (ch === 0x22 /* " */) inString = !inString;
    if (
      inString &&
      ch < 0x20 &&
      ch !== 0x09 /* tab */ &&
      ch !== 0x0a /* lf */ &&
      ch !== 0x0d /* cr */
    ) {
      continue;
    }
    out += data[i];
  }
  return out;
}

/** Convert `'foo'` to `"foo"`, but only if there are no double quotes yet. */
export function fixSingleQuotes(data: string): string {
  if (data.includes('"')) return data;
  let out = '';
  let inString = false;
  let escaped = false;
  for (let i = 0; i < data.length; i++) {
    const c = data[i];
    if (escaped) {
      out += c;
      escaped = false;
      continue;
    }
    if (c === '\\' && inString) {
      out += c;
      escaped = true;
      continue;
    }
    if (c === "'") {
      out += '"';
      inString = !inString;
      continue;
    }
    out += c;
  }
  return out;
}

/** Drop a trailing `,` before `}` or `]` (whitespace allowed in between). */
export function fixTrailingCommas(data: string): string {
  let out = '';
  let inString = false;
  let escaped = false;
  for (let i = 0; i < data.length; i++) {
    const c = data[i];
    if (escaped) {
      out += c;
      escaped = false;
      continue;
    }
    if (c === '\\' && inString) {
      out += c;
      escaped = true;
      continue;
    }
    if (c === '"') inString = !inString;

    if (c === ',' && !inString) {
      let j = i + 1;
      while (
        j < data.length &&
        (data[j] === ' ' || data[j] === '\t' || data[j] === '\n' || data[j] === '\r')
      ) {
        j++;
      }
      if (j < data.length && (data[j] === '}' || data[j] === ']')) {
        continue; // skip this comma
      }
    }
    out += c;
  }
  return out;
}

/** Append missing `}` / `]` characters (in reverse order of how they opened). */
export function fixUnmatchedBrackets(data: string): string {
  let inString = false;
  let escaped = false;
  const stack: string[] = [];
  for (let i = 0; i < data.length; i++) {
    const c = data[i];
    if (escaped) {
      escaped = false;
      continue;
    }
    if (c === '\\' && inString) {
      escaped = true;
      continue;
    }
    if (c === '"') {
      inString = !inString;
      continue;
    }
    if (inString) continue;
    if (c === '{') stack.push('}');
    else if (c === '[') stack.push(']');
    else if (c === '}' || c === ']') {
      if (stack.length > 0 && stack[stack.length - 1] === c) stack.pop();
    }
  }
  if (stack.length === 0) return data;
  let out = data;
  for (let i = stack.length - 1; i >= 0; i--) out += stack[i];
  return out;
}

/**
 * Merge `{a:1}\n{b:2}` -> `{"a":1,"b":2}`. If the input is already a single
 * valid JSON object/array we leave it alone.
 */
export function mergeJsonObjects(data: string): string {
  const trimmed = data.trim();
  if (isValidJson(trimmed)) return data;

  const lines = trimmed.split('\n');
  if (lines.length < 2) return data;

  const merged: Record<string, unknown> = {};
  let anyParsed = false;
  for (const raw of lines) {
    const line = raw.trim();
    if (line.length === 0) continue;
    try {
      const obj = JSON.parse(line);
      if (obj && typeof obj === 'object' && !Array.isArray(obj)) {
        Object.assign(merged, obj);
        anyParsed = true;
      }
    } catch {
      /* skip non-JSON lines */
    }
  }
  if (!anyParsed || Object.keys(merged).length === 0) return data;

  const sortedKeys = Object.keys(merged).sort();
  const ordered: Record<string, unknown> = {};
  for (const k of sortedKeys) ordered[k] = merged[k];
  try {
    return JSON.stringify(ordered);
  } catch {
    return data;
  }
}

function isValidJson(s: string): boolean {
  try {
    JSON.parse(s);
    return true;
  } catch {
    return false;
  }
}
