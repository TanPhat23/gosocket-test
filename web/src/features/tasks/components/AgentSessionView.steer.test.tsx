import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { AgentSessionView } from './AgentSessionView';
import { taskApi } from '../api/taskApi';
import type { TaskSessionResponse } from '../schemas/taskSchemas';

// Counts how often a transcript message body is rendered. Typing in the steer box must not re-render the transcript.
const markdownRenders = vi.hoisted(() => ({ count: 0 }));

vi.mock('@/components/ui/markdown', async () => {
  const React = await import('react');
  return {
    Markdown: ({ content }: { content: string }) => {
      markdownRenders.count += 1;
      return React.createElement('span', null, content);
    },
  };
});

const session: TaskSessionResponse = {
  task_id: 'task-steer-1',
  session_id: 'ses_steer_1',
  status: 'succeeded',
  model: 'gemini-3.8-flash',
  tokens: { input: 10, output: 5, reasoning: 0, cache_read: 0, cache_write: 0 },
  cost: 0,
  messages: [
    { id: 'u1', type: 'user', role: 'user', text: 'first prompt', created_at: 1791357502838 },
    { id: 'a1', type: 'assistant', role: 'assistant', text: 'first reply', created_at: 1791357502900 },
    { id: 'u2', type: 'user', role: 'user', text: 'second prompt', created_at: 1791357503000 },
    { id: 'a2', type: 'assistant', role: 'assistant', text: 'second reply', created_at: 1791357503100 },
  ],
};

describe('AgentSessionView steer input', () => {
  beforeEach(() => {
    markdownRenders.count = 0;
    vi.stubGlobal(
      'WebSocket',
      class {
        onmessage: unknown = null;
        onclose: unknown = null;
        onerror: unknown = null;
        constructor() {}
        close() {}
      }
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('does not re-render transcript messages on each keystroke in the steer box', async () => {
    vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(session);

    render(<AgentSessionView taskId="task-steer-1" />);
    await screen.findByTestId('session-transcript-container');
    const rendersAfterLoad = markdownRenders.count;
    expect(rendersAfterLoad).toBeGreaterThan(0);

    const textarea = screen.getByTestId('steer-prompt-textarea') as HTMLTextAreaElement;
    for (const value of ['f', 'fi', 'fix', 'fix ', 'fix t']) {
      fireEvent.change(textarea, { target: { value } });
    }

    expect(textarea).toHaveValue('fix t');
    expect(markdownRenders.count).toBe(rendersAfterLoad);
  });
});
