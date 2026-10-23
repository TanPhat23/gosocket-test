export interface WSMessage {
  task_id?: string;
  type?: 'log' | 'status' | 'diff' | 'agent_event' | string;
  payload?: unknown;
  timestamp?: string;
}

/**
 * Robustly parses incoming WebSocket frames that may contain:
 * 1. A single JSON message object
 * 2. Multiple concatenated JSON objects (e.g. `}{` boundary)
 * 3. Raw text or ANSI escape streams
 *
 * Correctly handles nested braces inside strings/code payloads.
 */
export function parseWsFrames(rawData: string): WSMessage[] {
  if (!rawData || typeof rawData !== 'string') return [];

  const trimmed = rawData.trim();
  if (!trimmed.startsWith('{')) {
    // Plain text or ANSI stream
    return [];
  }

  // Fast path: standard single JSON object
  try {
    const parsed = JSON.parse(trimmed);
    if (parsed && typeof parsed === 'object') {
      return [parsed as WSMessage];
    }
  } catch {
    // Might be multiple concatenated JSON objects
  }

  const frames: WSMessage[] = [];
  let depth = 0;
  let start = -1;
  let inString = false;
  let escape = false;

  for (let i = 0; i < rawData.length; i++) {
    const char = rawData[i];

    if (escape) {
      escape = false;
      continue;
    }

    if (char === '\\') {
      escape = true;
      continue;
    }

    if (char === '"') {
      inString = !inString;
      continue;
    }

    if (!inString) {
      if (char === '{') {
        if (depth === 0) start = i;
        depth++;
      } else if (char === '}') {
        depth--;
        if (depth === 0 && start !== -1) {
          const jsonStr = rawData.substring(start, i + 1);
          try {
            const parsed = JSON.parse(jsonStr);
            if (parsed && typeof parsed === 'object') {
              frames.push(parsed as WSMessage);
            }
          } catch {
            // Ignore corrupted chunk
          }
          start = -1;
        }
      }
    }
  }

  return frames;
}
