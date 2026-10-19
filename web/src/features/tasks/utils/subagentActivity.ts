import type { SessionChild, SessionMessage, SessionToolCall } from '../schemas/taskSchemas';
import { parseSessionMessageContent } from './sessionMessage';

// The newest steps of a subagent, oldest first.
const MAX_STEPS = 30;

export type SubagentStep =
  | { key: string; kind: 'tool'; name: string; detail: string; status?: string }
  | { key: string; kind: 'text'; text: string };

// The first string field of a tool input that names what the tool acted on.
function toolDetail(input: SessionToolCall['input']): string {
  if (!input) return '';
  if (typeof input === 'string') return input;
  for (const key of ['command', 'description', 'filePath', 'path', 'pattern']) {
    const value = input[key];
    if (typeof value === 'string' && value) return value;
  }
  return '';
}

// A subagent's tool calls and the text it wrote, oldest first, limited to the newest steps.
export function subagentSteps(messages: SessionMessage[] = []): SubagentStep[] {
  const steps: SubagentStep[] = [];
  for (const msg of messages) {
    const parsed = parseSessionMessageContent(msg);
    parsed.toolCalls.forEach((tc, i) => {
      steps.push({ key: `${msg.id}-tool-${i}`, kind: 'tool', name: tc.name, detail: toolDetail(tc.input), status: tc.status });
    });
    const text = parsed.text.trim();
    if (text) steps.push({ key: `${msg.id}-text`, kind: 'text', text });
  }
  return steps.slice(-MAX_STEPS);
}

function stepLine(step: SubagentStep): string {
  if (step.kind === 'text') return step.text;
  return step.detail ? `→ ${step.name}  ${step.detail}` : `→ ${step.name}`;
}

// The latest step, for a one-line status on the subagent's tool card.
export function subagentLatestStep(child: SessionChild): string {
  const steps = subagentSteps(child.messages);
  const last = steps[steps.length - 1];
  if (!last) return '';
  const line = stepLine(last).split('\n')[0];
  return line.length > 140 ? `${line.slice(0, 140)}…` : line;
}

// The subagent's activity as the text the inspector shows while the subagent runs.
export function subagentTranscript(child: SessionChild): string {
  return subagentSteps(child.messages).map(stepLine).join('\n\n');
}

// The child session behind a subagent tool call. The tool result names it; when it does not, fall back to the child
// whose title carries the task description, since OpenCode titles a subagent session after the task it was given.
export function findSubagentChild(children: SessionChild[] | undefined, tc: SessionToolCall): SessionChild | undefined {
  if (!children?.length) return undefined;
  if (tc.session_id) return children.find((c) => c.id === tc.session_id);
  const description = typeof tc.input === 'object' && tc.input !== null ? String(tc.input.description ?? '').trim() : '';
  if (!description) return undefined;
  const matches = children.filter((c) => c.title?.includes(description));
  return matches.length === 1 ? matches[0] : undefined;
}
