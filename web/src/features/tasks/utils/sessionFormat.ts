import type { SessionMessage } from '../schemas/taskSchemas';

export const formatTimestamp = (ts?: number | string | null) => {
  if (!ts) return null;
  try {
    return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  } catch {
    return null;
  }
};

export function sessionErrorMessage(err: { status?: number; message?: string }): string {
  if (err.status === 503) return 'Task execution is temporarily disabled by the server.';
  if (err.status === 404) return 'No active session initialized for this task yet.';
  return err.message || 'Failed to load task session.';
}

// A tool call's input or output as the text the UI shows: strings as they are, objects as indented JSON.
export function toDisplayText(value: string | Record<string, unknown> | undefined): string {
  if (typeof value === 'string') return value;
  return value ? JSON.stringify(value, null, 2) : '';
}

// The text of a user message, wherever the session API put it.
export function userPromptText(msg: SessionMessage, parsedText: string): string {
  let text = parsedText || (typeof msg.text === 'string' ? msg.text : '') || (typeof msg.content === 'string' ? msg.content : '');
  if (!text && typeof msg.content === 'object' && msg.content !== null) {
    const content = msg.content as Record<string, unknown>;
    if (typeof content.text === 'string') text = content.text;
  }
  return text;
}
