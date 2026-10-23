import { describe, it, expect } from 'vitest';
import { parseWsFrames } from './terminalParser';

describe('terminalParser', () => {
  it('parses a single JSON log message', () => {
    const raw = JSON.stringify({
      task_id: 'task-1',
      type: 'log',
      payload: 'Compiling project...\r\n',
      timestamp: '2026-10-06T14:00:00Z',
    });

    const frames = parseWsFrames(raw);
    expect(frames).toHaveLength(1);
    expect(frames[0].type).toBe('log');
    expect(frames[0].payload).toBe('Compiling project...\r\n');
  });

  it('parses multiple concatenated JSON messages in a single buffer', () => {
    const msg1 = JSON.stringify({
      task_id: 'task-1',
      type: 'log',
      payload: 'chunk 1\r\n',
      timestamp: '2026-10-06T14:00:00Z',
    });
    const msg2 = JSON.stringify({
      task_id: 'task-1',
      type: 'log',
      payload: 'chunk 2\r\n',
      timestamp: '2026-10-06T14:00:01Z',
    });
    const raw = `${msg1}${msg2}`;

    const frames = parseWsFrames(raw);
    expect(frames).toHaveLength(2);
    expect(frames[0].payload).toBe('chunk 1\r\n');
    expect(frames[1].payload).toBe('chunk 2\r\n');
  });

  it('correctly handles code payloads containing curly braces and escaped characters', () => {
    const code = 'int main(void) {\n    printf("{Hello, World!}\\n");\n    return 0;\n}';
    const raw = JSON.stringify({
      task_id: 'task-code',
      type: 'log',
      payload: code,
      timestamp: '2026-10-06T14:00:00Z',
    });

    const frames = parseWsFrames(raw);
    expect(frames).toHaveLength(1);
    expect(frames[0].payload).toBe(code);
  });

  it('parses status change messages', () => {
    const raw = JSON.stringify({
      task_id: 'task-1',
      type: 'status',
      payload: 'in_progress',
      timestamp: '2026-10-06T14:00:00Z',
    });

    const frames = parseWsFrames(raw);
    expect(frames).toHaveLength(1);
    expect(frames[0].type).toBe('status');
    expect(frames[0].payload).toBe('in_progress');
  });

  it('returns empty array for plain text or non-json input', () => {
    expect(parseWsFrames('Just a plain text stream')).toHaveLength(0);
    expect(parseWsFrames('\x1b[32mANSI colored output\x1b[0m')).toHaveLength(0);
    expect(parseWsFrames('')).toHaveLength(0);
  });

  it('parses structured agent_event frames', () => {
    const raw = JSON.stringify({
      task_id: 'task-1',
      type: 'agent_event',
      payload: { eventType: 'tool_use', toolName: 'shell', toolStatus: 'running' },
    });
    const frames = parseWsFrames(raw);
    expect(frames).toHaveLength(1);
    expect(frames[0].type).toBe('agent_event');
    expect((frames[0].payload as { toolName: string }).toolName).toBe('shell');
  });
});
