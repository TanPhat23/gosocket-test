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
