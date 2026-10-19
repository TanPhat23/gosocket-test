import { describe, it, expect } from 'vitest';
import type { SessionChild, SessionMessage } from '../schemas/taskSchemas';
import { findSubagentChild, subagentLatestStep, subagentSteps, subagentTranscript } from './subagentActivity';

const shell = (id: string, command: string): SessionMessage => ({
  id,
  role: 'assistant',
  content: [{ type: 'tool', tool: 'shell', state: { status: 'completed', input: { command } } }],
});

describe('subagentSteps', () => {
  it('lists tool calls and text oldest first', () => {
    const steps = subagentSteps([shell('a', 'go test ./...'), { id: 'b', role: 'assistant', text: 'All green.' }]);

    expect(steps.map((s) => (s.kind === 'tool' ? `${s.name} ${s.detail}` : s.text))).toEqual(['shell go test ./...', 'All green.']);
  });

  it('keeps only the newest 30 steps', () => {
    const messages = Array.from({ length: 40 }, (_, i) => shell(`m${i}`, `step ${i}`));

    const steps = subagentSteps(messages);

    expect(steps).toHaveLength(30);
    expect(steps[0].kind === 'tool' && steps[0].detail).toBe('step 10');
  });
});

describe('subagentLatestStep and subagentTranscript', () => {
  const child: SessionChild = { id: 'c1', title: 'backend', messages: [shell('a', 'go vet ./...'), shell('b', 'go test ./...')] };

  it('gives the latest step as one line', () => {
    expect(subagentLatestStep(child)).toBe('→ shell  go test ./...');
  });

  it('gives a child with no activity an empty latest step', () => {
    expect(subagentLatestStep({ id: 'c2' })).toBe('');
  });

  it('renders the whole activity as the inspector text', () => {
    expect(subagentTranscript(child)).toBe('→ shell  go vet ./...\n\n→ shell  go test ./...');
  });
});

describe('findSubagentChild', () => {
  const kids: SessionChild[] = [
    { id: 'c1', title: 'Write the tests (@backend subagent)' },
    { id: 'c2', title: 'Review the change (@quality subagent)' },
  ];

  it('prefers the session id named by the tool result', () => {
    expect(findSubagentChild(kids, { name: 'subagent', session_id: 'c2' })?.id).toBe('c2');
  });

  it('falls back to the one child whose title carries the task description', () => {
    expect(findSubagentChild(kids, { name: 'subagent', input: { description: 'Review the change' } })?.id).toBe('c2');
  });

  it('gives up when the description matches nothing or several children', () => {
    expect(findSubagentChild(kids, { name: 'subagent', input: { description: 'Deploy' } })).toBeUndefined();
    expect(findSubagentChild(kids, { name: 'subagent', input: { description: 'the' } })).toBeUndefined();
    expect(findSubagentChild(undefined, { name: 'subagent', session_id: 'c1' })).toBeUndefined();
  });
});
