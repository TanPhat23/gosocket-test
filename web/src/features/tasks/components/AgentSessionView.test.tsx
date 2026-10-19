import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { AgentSessionView, parseSessionMessageContent } from './AgentSessionView';
import { taskApi } from '../api/taskApi';
import type { SessionMessage, TaskSessionResponse } from '../schemas/taskSchemas';

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

  it('extracts question tool calls and parses state input options', () => {
    const rawContent = [
      {
        type: 'tool',
        name: 'question',
        state: {
          status: 'running',
          input: {
            questions: [
              {
                header: 'Design Direction',
                question: 'Which visual aesthetic should be applied?',
                options: [
                  { label: 'Deep Blue (Recommended)', description: 'Minimalist blue palette' },
                  { label: 'Monochrome Slate', description: 'Clean neutral black and white' },
                ],
                multiple: false,
              },
            ],
          },
        },
      },
    ];

    const msg: SessionMessage = {
      id: 'msg_q',
      role: 'assistant',
      content: rawContent,
    };

    const parsed = parseSessionMessageContent(msg);

    expect(parsed.toolCalls).toHaveLength(1);
    expect(parsed.toolCalls[0].name).toBe('question');
    expect(parsed.toolCalls[0].status).toBe('running');
    expect(parsed.toolCalls[0].input).toBeDefined();
    const inputObj = parsed.toolCalls[0].input as { questions: unknown[] };
    expect(inputObj.questions).toHaveLength(1);
  });

  it('extracts structured questionnaire from markdown text (Image 2 coordinator clarification turn)', () => {
    const markdownMessage = `
### Classification: Bounded Task

The project is a standard Next.js (App Router, Tailwind CSS v4, React 19) template application. We want to add a hero banner section to the UI.

Before proceeding, let's clarify your preferences:

1. **Placement & Scope:**
- **Option A (Recommended):** Create a dedicated, clean hero banner section on \`src/app/page.tsx\` (headline, subheading/tagline, primary and secondary call-to-action buttons, badge/pill), replacing or integrating cleanly with the default Next.js starter layout.
- **Option B:** Add a reusable \`src/components/HeroBanner.tsx\` component and mount it at the top of \`src/app/page.tsx\`.

2. **Copy & Purpose:**
- What is the app's topic or brand name, or would you like a modern tech/product placeholder (e.g. "Build faster with Agent Task Manager")?

Which direction do you prefer?
`;

    const msg: SessionMessage = {
      id: 'msg_clarify',
      role: 'assistant',
      text: markdownMessage,
    };

    const parsed = parseSessionMessageContent(msg);

    expect(parsed.extractedQuestions).toBeDefined();
    expect(parsed.extractedQuestions).toHaveLength(2);
    expect(parsed.extractedQuestions?.[0].header).toBe('1. Placement & Scope');
    expect(parsed.extractedQuestions?.[0].options).toHaveLength(2);
    expect(parsed.extractedQuestions?.[0].options?.[0].label).toBe('Option A (Recommended)');
    expect(parsed.extractedQuestions?.[1].header).toBe('2. Copy & Purpose');
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

describe('subagent tool calls', () => {
  it('reads the child session ID from the tool metadata', () => {
    const msg = {
      id: 'm-sub',
      role: 'assistant',
      content: [
        {
          type: 'tool',
          tool: 'subagent',
          state: { status: 'running', input: { description: 'review' }, metadata: { sessionId: 'ses_child_9' } },
        },
      ],
    } as unknown as SessionMessage;

    expect(parseSessionMessageContent(msg).toolCalls[0].session_id).toBe('ses_child_9');
  });
});

describe('AgentSessionView Split Two-Pane Inspector', () => {
  const mockTaskId = 'task-test-split-1';

  const mockSessionData: TaskSessionResponse = {
    task_id: mockTaskId,
    session_id: 'ses_atm_split_1',
    status: 'idle',
    model: 'gemini-3.8-flash',
    tokens: {
      input: 5000,
      output: 1200,
      reasoning: 200,
      cache_read: 1500,
      cache_write: 0,
    },
    cost: 0.0042,
    messages: [
      {
        id: 'msg_u1',
        type: 'user',
        role: 'user',
        text: 'Fix the failing test in agent service',
        created_at: 1791357500000,
      },
      {
        id: 'msg_a1',
        type: 'assistant',
        role: 'assistant',
        agent: 'coordinator',
        model: 'gemini-3.8-flash',
        text: 'I ran the shell command and encountered an issue.',
        tool_calls: [
          {
            name: 'shell',
            input: { command: 'go test ./pkg/agent/...' },
            output: 'FAIL: TestAgentSessionRunner (0.42s)\n    agent_test.go:45: expected nil, got error 500',
            status: 'error',
            exit_code: 1,
          },
          {
            name: 'read',
            input: { path: 'pkg/agent/runner.go' },
            output: 'package agent\n\nfunc Run() error { return nil }',
            status: 'completed',
            exit_code: 0,
          },
        ],
        created_at: 1791357510000,
      },
    ],
  };

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('by default does not show tool-inspector-panel until a tool card is clicked', async () => {
    vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(mockSessionData);

    render(<AgentSessionView taskId={mockTaskId} />);

    await waitFor(() => {
      expect(screen.getByTestId('session-transcript-container')).toBeInTheDocument();
    });

    // Inspector panel should NOT be visible by default
    expect(screen.queryByTestId('tool-inspector-panel')).not.toBeInTheDocument();

    // Transcript messages
    expect(screen.getByTestId('user-message')).toHaveTextContent('Fix the failing test in agent service');
    expect(screen.getByTestId('assistant-message')).toHaveTextContent('I ran the shell command and encountered an issue.');

    // Clicking tool call card opens tool-inspector-panel
    const toolCards = screen.getAllByTestId('tool-call-card');
    expect(toolCards.length).toBe(2);
    fireEvent.click(toolCards[0]);

    await waitFor(() => {
      expect(screen.getByTestId('tool-inspector-panel')).toBeInTheDocument();
      expect(screen.getByTestId('tool-inspector-panel')).toHaveTextContent('shell');
      expect(screen.getByText('Detected Error')).toBeInTheDocument();
    });
  });

  it('sends an interrupt for the task session without cancelling the task', async () => {
    vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(mockSessionData);
    const interrupt = vi.spyOn(taskApi, 'interruptTaskSession').mockResolvedValue(undefined);
    const cancel = vi.spyOn(taskApi, 'cancelTask');

    render(<AgentSessionView taskId={mockTaskId} />);

    await waitFor(() => {
      expect(screen.getByTestId('session-transcript-container')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('interrupt-session-btn'));

    await waitFor(() => {
      expect(interrupt).toHaveBeenCalledWith(mockTaskId);
    });
    expect(cancel).not.toHaveBeenCalled();
    await waitFor(() => {
      expect(screen.getByText('Interrupt sent. The current turn is stopping.')).toBeInTheDocument();
    });
  });

  it('shows the error when the interrupt fails', async () => {
    vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(mockSessionData);
    vi.spyOn(taskApi, 'interruptTaskSession').mockRejectedValue(new Error('sandbox is not running'));

    render(<AgentSessionView taskId={mockTaskId} />);

    await waitFor(() => {
      expect(screen.getByTestId('session-transcript-container')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('interrupt-session-btn'));

    await waitFor(() => {
      expect(screen.getByText('Interrupt failed: sandbox is not running')).toBeInTheDocument();
    });
  });

  it('applies snapshots pushed over the session stream', async () => {
    vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(mockSessionData);
    const sockets: Array<{ url: string; onmessage?: (e: { data: string }) => void; close: () => void }> = [];
    class FakeSocket {
      url: string;
      onmessage?: (e: { data: string }) => void;
      onclose?: () => void;
      onerror?: () => void;
      constructor(url: string) {
        this.url = url;
        sockets.push(this);
      }
      close() {}
    }
    vi.stubGlobal('WebSocket', FakeSocket);
    try {
      render(<AgentSessionView taskId={mockTaskId} />);
      await waitFor(() => {
        expect(screen.getByTestId('session-transcript-container')).toBeInTheDocument();
      });
      const stream = sockets.find((s) => s.url.includes('/ws/session/'));
      expect(stream).toBeDefined();
      stream!.onmessage?.({
        data: JSON.stringify({
          type: 'session',
          session: {
            ...mockSessionData,
            messages: [{ id: 'frame_msg', role: 'assistant', text: 'Streamed reply from the session stream' }],
          },
        }),
      });
      await waitFor(() => {
        expect(screen.getByText('Streamed reply from the session stream')).toBeInTheDocument();
      });
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it('allows clicking another tool call card to inspect its output in the inspector', async () => {
    vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(mockSessionData);

    render(<AgentSessionView taskId={mockTaskId} />);

    await waitFor(() => {
      expect(screen.getByTestId('session-transcript-container')).toBeInTheDocument();
    });

    // Click second tool call card ('read')
    const toolCards = screen.getAllByTestId('tool-call-card');
    expect(toolCards.length).toBe(2);

    fireEvent.click(toolCards[1]);

    // Inspector should open with 'read' and its output
    await waitFor(() => {
      const inspector = screen.getByTestId('tool-inspector-panel');
      expect(inspector).toHaveTextContent('read');
      expect(inspector).toHaveTextContent('func Run() error { return nil }');
    });
  });

  it('closes and unselects inspector when clicking Close button', async () => {
    vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(mockSessionData);

    render(<AgentSessionView taskId={mockTaskId} />);

    await waitFor(() => {
      expect(screen.getByTestId('session-transcript-container')).toBeInTheDocument();
    });

    // Click tool card to open
    const toolCards = screen.getAllByTestId('tool-call-card');
    fireEvent.click(toolCards[0]);

    await waitFor(() => {
      expect(screen.getByTestId('tool-inspector-panel')).toBeInTheDocument();
    });

    // Click Close / Unselect via footer or header close button
    const closeBtn = screen.getByTestId('close-inspector-footer-btn');
    fireEvent.click(closeBtn);

    // Inspector panel should be dismissed
    expect(screen.queryByTestId('tool-inspector-panel')).not.toBeInTheDocument();

    // Reopen and test header close button as well
    fireEvent.click(toolCards[0]);
    await waitFor(() => {
      expect(screen.getByTestId('tool-inspector-panel')).toBeInTheDocument();
    });
    const headerCloseBtn = screen.getByTestId('close-inspector-header-btn');
    fireEvent.click(headerCloseBtn);
    expect(screen.queryByTestId('tool-inspector-panel')).not.toBeInTheDocument();
  });

  it('displays scroll-to-bottom button when scrolled up and jumps to bottom on click', async () => {
    vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(mockSessionData);
    const scrollIntoViewMock = vi.fn();
    window.HTMLElement.prototype.scrollIntoView = scrollIntoViewMock;

    render(<AgentSessionView taskId={mockTaskId} />);

    await waitFor(() => {
      expect(screen.getByTestId('session-transcript-container')).toBeInTheDocument();
    });

    const container = screen.getByTestId('session-transcript-container');

    // Initially at bottom, scroll button not visible
    expect(screen.queryByTestId('scroll-to-bottom-btn')).not.toBeInTheDocument();

    // Simulate scrolling up
    Object.defineProperty(container, 'scrollHeight', { value: 1000, configurable: true, writable: true });
    Object.defineProperty(container, 'clientHeight', { value: 400, configurable: true, writable: true });
    Object.defineProperty(container, 'scrollTop', { value: 200, configurable: true, writable: true }); // distanceFromBottom = 400 > 100

    fireEvent.scroll(container);

    await waitFor(() => {
      expect(screen.getByTestId('scroll-to-bottom-btn')).toBeInTheDocument();
    });

    // Click scroll to bottom button
    fireEvent.click(screen.getByTestId('scroll-to-bottom-btn'));
    expect(scrollIntoViewMock).toHaveBeenCalled();
  });

  it('supports "Insert Fix into Steer Input" button to prefill steer prompt', async () => {
    vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(mockSessionData);

    render(<AgentSessionView taskId={mockTaskId} />);

    await waitFor(() => {
      expect(screen.getByTestId('session-transcript-container')).toBeInTheDocument();
    });

    // Open first tool card to access inspector
    const toolCards = screen.getAllByTestId('tool-call-card');
    fireEvent.click(toolCards[0]);

    await waitFor(() => {
      expect(screen.getByTestId('insert-fix-btn')).toBeInTheDocument();
    });

    const insertFixBtn = screen.getByTestId('insert-fix-btn');
    fireEvent.click(insertFixBtn);

    const steerTextarea = screen.getByTestId('steer-prompt-textarea');
    expect(steerTextarea).toHaveValue(
      'Fix the error in tool "shell":\nFAIL: TestAgentSessionRunner (0.42s)\n    agent_test.go:45: expected nil, got error 500\nPlease resolve and re-run.'
    );
  });

  it('populates steer textarea when clicking suggestion chips', async () => {
    vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(mockSessionData);

    render(<AgentSessionView taskId={mockTaskId} />);

    await waitFor(() => {
      expect(screen.getByText('Fix failed step')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Fix failed step'));

    const steerTextarea = screen.getByTestId('steer-prompt-textarea');
    expect(steerTextarea).toHaveValue('Fix the failed step and run tests again.');
  });

  it('renders markdown in Prompt Brief / Input tab and subagent prompt card with toggle', async () => {
    const subagentSessionData: TaskSessionResponse = {
      task_id: mockTaskId,
      session_id: 'ses_atm_subagent_1',
      status: 'idle',
      model: 'gemini-3.8-flash',
      messages: [
        {
          id: 'msg_sub_1',
          type: 'assistant',
          role: 'assistant',
          agent: 'coordinator',
          text: 'Spawning subagent for bounded UI implementation.',
          tool_calls: [
            {
              name: 'subagent',
              input: {
                agent: 'frontend',
                description: 'Implement markdown parsing and inspector enhancements',
                prompt: '### Subagent Brief\n\n- Fix tab auto-scroll\n- Render markdown in `<Markdown />`',
              },
              output: '### Subagent Execution Report\n\nAll tasks verified and tests passing.',
              status: 'completed',
              exit_code: 0,
            },
          ],
        },
      ],
    };

    vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(subagentSessionData);

    render(<AgentSessionView taskId={mockTaskId} />);

    await waitFor(() => {
      expect(screen.getByTestId('session-transcript-container')).toBeInTheDocument();
    });

    // Click subagent tool card
    const toolCards = screen.getAllByTestId('tool-call-card');
    fireEvent.click(toolCards[0]);

    await waitFor(() => {
      expect(screen.getByTestId('tool-inspector-panel')).toBeInTheDocument();
    });

    // In output tab, markdown toggle is visible for subagent
    expect(screen.getByTestId('toggle-output-markdown-btn')).toBeInTheDocument();
    expect(screen.getByText('Subagent Execution Report')).toBeInTheDocument();

    // Switch to input tab
    fireEvent.click(screen.getByText('Prompt Brief / Input'));

    // Subagent objective badge/card
    expect(screen.getByText('Subagent Objective:')).toBeInTheDocument();
    expect(screen.getAllByText('Implement markdown parsing and inspector enhancements').length).toBeGreaterThan(0);

    // Formatted Brief Markdown view
    expect(screen.getByTestId('toggle-input-markdown-btn')).toBeInTheDocument();
    expect(screen.getByText('Subagent Brief')).toBeInTheDocument();

    // Toggle to Raw Input view
    fireEvent.click(screen.getByTestId('toggle-input-raw-btn'));
    expect(screen.getByText(/"agent":\s*"frontend"/)).toBeInTheDocument();
  });

  it('attaches an image to the steer and sends it with the prompt', async () => {
    const promptSpy = vi.spyOn(taskApi, 'promptTaskSession').mockResolvedValue(undefined);
    vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(mockSessionData);

    render(<AgentSessionView taskId={mockTaskId} />);

    const input = await screen.findByTestId('steer-image-input');
    fireEvent.change(input, { target: { files: [new File(['png-bytes'], 'shot.png', { type: 'image/png' })] } });

    expect(await screen.findByTestId('steer-image-list')).toHaveTextContent('');
    expect(screen.getByAltText('shot.png')).toBeInTheDocument();

    fireEvent.change(screen.getByTestId('steer-prompt-textarea'), { target: { value: 'look at this' } });
    fireEvent.click(screen.getByTestId('send-steer-btn'));

    await waitFor(() => {
      expect(promptSpy).toHaveBeenCalledWith(mockTaskId, 'look at this', [
        expect.objectContaining({ mime: 'image/png', name: 'shot.png', data: btoa('png-bytes') }),
      ]);
    });
  });

  it('shows a running subagent card the latest step of its own child session', async () => {
    const session: TaskSessionResponse = {
      task_id: mockTaskId,
      session_id: 'ses_atm_linked',
      status: 'running',
      model: 'gemini-3.8-flash',
      messages: [
        {
          id: 'msg_parent',
          role: 'assistant',
          content: [
            {
              type: 'tool',
              tool: 'subagent',
              state: {
                status: 'running',
                input: { agent: 'backend', description: 'Improve gosocket' },
                metadata: { sessionId: 'ses_child_live' },
              },
            },
          ],
        },
      ],
      children: [
        {
          id: 'ses_child_live',
          title: 'backend',
          status: 'running',
          messages: [
            {
              id: 'child_m1',
              role: 'assistant',
              content: [{ type: 'tool', tool: 'shell', state: { status: 'completed', input: { command: 'go test ./...' } } }],
            },
          ],
        },
      ],
    };
    vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(session);

    render(<AgentSessionView taskId={mockTaskId} />);

    fireEvent.click(await screen.findByTestId('toggle-tool-subagent'));
    expect(await screen.findByText('→ shell go test ./...')).toBeInTheDocument();
    expect(screen.queryByTestId('toggle-subagents-btn')).not.toBeInTheDocument();
  });

  it('shows the activity of a finished subagent in the inspector, linked by its task description', async () => {
    const session: TaskSessionResponse = {
      task_id: mockTaskId,
      session_id: 'ses_atm_done',
      status: 'idle',
      model: 'gemini-3.8-flash',
      messages: [
        {
          id: 'msg_parent',
          role: 'assistant',
          content: [
            {
              type: 'tool',
              tool: 'subagent',
              state: { status: 'completed', input: { agent: 'backend', description: 'Improve gosocket' }, output: 'Done.' },
            },
          ],
        },
      ],
      children: [
        {
          id: 'ses_child_done',
          title: 'Improve gosocket (@backend subagent)',
          status: 'idle',
          messages: [
            {
              id: 'child_m1',
              role: 'assistant',
              content: [{ type: 'tool', tool: 'shell', state: { status: 'completed', input: { command: 'go test ./...' } } }],
            },
            { id: 'child_m2', role: 'assistant', text: 'Race detector is clean.' },
          ],
        },
      ],
    };
    vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(session);

    render(<AgentSessionView taskId={mockTaskId} />);

    fireEvent.click(await screen.findByTestId('tool-call-card'));
    const timeline = await screen.findByTestId('subagent-activity');
    expect(timeline).toHaveTextContent('go test ./...');
    expect(timeline).toHaveTextContent('Race detector is clean.');

    fireEvent.click(screen.getByText('Terminal Output (Stdout/Stderr)'));
    expect(screen.queryByTestId('subagent-activity')).not.toBeInTheDocument();
  });

  it('defaults inspectorTab to input when subagent has no output yet', async () => {
    const runningSubagentSession: TaskSessionResponse = {
      task_id: mockTaskId,
      session_id: 'ses_atm_subagent_2',
      status: 'running',
      model: 'gemini-3.8-flash',
      messages: [
        {
          id: 'msg_sub_2',
          type: 'assistant',
          role: 'assistant',
          text: 'Dispatching background work...',
          tool_calls: [
            {
              name: 'subagent',
              input: {
                agent: 'frontend',
                description: 'Ongoing work',
                prompt: '### Active Task Brief',
              },
              output: undefined,
              status: 'running',
            },
          ],
        },
      ],
    };

    vi.spyOn(taskApi, 'getTaskSession').mockResolvedValue(runningSubagentSession);

    render(<AgentSessionView taskId={mockTaskId} />);

    await waitFor(() => {
      expect(screen.getByTestId('session-transcript-container')).toBeInTheDocument();
    });

    // Click subagent card
    const toolCards = screen.getAllByTestId('tool-call-card');
    fireEvent.click(toolCards[0]);

    await waitFor(() => {
      expect(screen.getByTestId('tool-inspector-panel')).toBeInTheDocument();
    });

    // Should default to input tab so user immediately sees prompt brief in markdown
    expect(screen.getByText('Active Task Brief')).toBeInTheDocument();
  });
});
