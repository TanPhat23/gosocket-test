import { describe, it, expect } from 'vitest';
import { parseSessionMessageContent } from './AgentSessionView';
import type { SessionMessage } from '../schemas/taskSchemas';

describe('parseSessionMessageContent', () => {
  it('extracts text from an array of parts with state/thoughtSignature (Gemini/OpenCode format)', () => {
    const rawContent = [
      {
        state: {
          thoughtSignature: 'EmoKaAFpFHOTH...abc123456',
        },
        text: '### Design Proposal: Modern Hero Section for `src/app/page.tsx`\n\n#### 1. Scope & Intent\nReplace the default Next.js starter content with a modern Hero section.',
        type: 'text',
      },
    ];

    const msg: SessionMessage = {
      id: 'msg_1',
      role: 'assistant',
      content: rawContent,
    };

    const parsed = parseSessionMessageContent(msg);

    expect(parsed.text).toBe(
      '### Design Proposal: Modern Hero Section for `src/app/page.tsx`\n\n#### 1. Scope & Intent\nReplace the default Next.js starter content with a modern Hero section.'
    );
    expect(parsed.text).not.toContain('thoughtSignature');
    expect(parsed.text).not.toContain('"type": "text"');
    expect(parsed.toolCalls).toHaveLength(0);
  });

  it('extracts text from stringified JSON content', () => {
    const rawContentStr = JSON.stringify([
      {
        type: 'text',
        text: 'This is formatted content from JSON string',
      },
    ]);

    const msg: SessionMessage = {
      id: 'msg_2',
      role: 'assistant',
      content: rawContentStr,
    };

    const parsed = parseSessionMessageContent(msg);

    expect(parsed.text).toBe('This is formatted content from JSON string');
    expect(parsed.text).not.toContain('[');
    expect(parsed.text).not.toContain(']');
  });

  it('extracts reasoning / thoughts into thoughts field when present', () => {
    const rawContent = [
      {
        type: 'thought',
        thought: 'I should verify the requirements before suggesting changes.',
      },
      {
        type: 'text',
        text: 'Here is my recommendation for the hero banner.',
      },
    ];

    const msg: SessionMessage = {
      id: 'msg_3',
      role: 'assistant',
      content: rawContent,
    };

    const parsed = parseSessionMessageContent(msg);

    expect(parsed.text).toBe('Here is my recommendation for the hero banner.');
    expect(parsed.thoughts).toBe('I should verify the requirements before suggesting changes.');
  });

  it('extracts embedded tool calls from content parts', () => {
    const rawContent = [
      {
        type: 'text',
        text: 'Running checks...',
      },
      {
        type: 'tool_use',
        name: 'shell',
        input: { command: 'npm test' },
        status: 'completed',
        exit_code: 0,
      },
    ];

    const msg: SessionMessage = {
      id: 'msg_4',
      role: 'assistant',
      content: rawContent,
    };

    const parsed = parseSessionMessageContent(msg);

    expect(parsed.text).toBe('Running checks...');
    expect(parsed.toolCalls).toHaveLength(1);
    expect(parsed.toolCalls[0].name).toBe('shell');
    expect(parsed.toolCalls[0].input).toEqual({ command: 'npm test' });
  });

  it('handles plain text and user messages cleanly', () => {
    const msg: SessionMessage = {
      id: 'msg_5',
      role: 'user',
      text: 'Add hero banner section to UI',
    };

    const parsed = parseSessionMessageContent(msg);

    expect(parsed.text).toBe('Add hero banner section to UI');
    expect(parsed.thoughts).toBeUndefined();
    expect(parsed.toolCalls).toHaveLength(0);
  });
});
