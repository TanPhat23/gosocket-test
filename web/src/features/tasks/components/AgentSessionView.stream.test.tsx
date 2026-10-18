import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, act } from '@testing-library/react';
import { AgentSessionView } from './AgentSessionView';
import { taskApi } from '../api/taskApi';
import type { TaskSessionResponse } from '../schemas/taskSchemas';

class FakeSocket {
  static instances: FakeSocket[] = [];
  url: string;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;

  constructor(url: string) {
    this.url = url;
    FakeSocket.instances.push(this);
  }

  close() {}
}

const socketFor = (fragment: string) => FakeSocket.instances.find((s) => s.url.includes(fragment));

const emit = (socket: FakeSocket | undefined, payload: unknown) => {
  if (!socket?.onmessage) throw new Error('socket has no message handler');
  socket.onmessage({ data: JSON.stringify(payload) });
};

const settle = () => act(async () => {
  await new Promise((resolve) => setTimeout(resolve, 20));
});

const session: TaskSessionResponse = {
  task_id: 'task-live-1',
  session_id: 'ses_live_1',
  status: 'succeeded',
  model: 'gemini-3.8-flash',
  tokens: { input: 10, output: 5, reasoning: 0, cache_read: 0, cache_write: 0 },
  cost: 0,
  messages: [{ id: 'u1', type: 'user', role: 'user', text: 'hello agent', created_at: 1791357502838 }],
};

describe('AgentSessionView session fetching', () => {
  beforeEach(() => {
    FakeSocket.instances = [];
    vi.stubGlobal('WebSocket', FakeSocket);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('does not refetch on terminal agent_event frames while the session stream is live', async () => {
    const getSession = vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(session);

    render(<AgentSessionView taskId="task-live-1" />);
    await screen.findByTestId('session-transcript-container');
    expect(getSession).toHaveBeenCalledTimes(1);

    act(() => {
      emit(socketFor('/ws/session/'), { type: 'session', session });
    });

    const terminal = socketFor('/ws/terminal/');
    act(() => {
      emit(terminal, { type: 'agent_event', payload: { eventType: 'message_part' } });
      emit(terminal, { type: 'agent_event', payload: { eventType: 'message_part' } });
      emit(terminal, { type: 'agent_event', payload: { eventType: 'tool_use' } });
    });
    await settle();

    expect(getSession).toHaveBeenCalledTimes(1);
  });

  it('refetches once on a terminal agent_event when the session stream is not live', async () => {
    const getSession = vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(session);

    render(<AgentSessionView taskId="task-live-1" />);
    await screen.findByTestId('session-transcript-container');
    expect(getSession).toHaveBeenCalledTimes(1);

    act(() => {
      emit(socketFor('/ws/terminal/'), { type: 'agent_event', payload: { eventType: 'message_part' } });
    });
    await waitFor(() => expect(getSession).toHaveBeenCalledTimes(2));
  });
});
