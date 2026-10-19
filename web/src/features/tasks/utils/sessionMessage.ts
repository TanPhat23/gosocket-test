import type { SessionMessage, SessionToolCall } from '../schemas/taskSchemas';
import type { QuestionItemData } from '../components/AgentQuestionnaire';
import { extractQuestionsFromText } from './questionParser';

export interface ParsedMessageData {
  text: string;
  thoughts?: string;
  toolCalls: SessionToolCall[];
  extractedQuestions?: QuestionItemData[];
}

export function parseSessionMessageContent(msg: SessionMessage): ParsedMessageData {
  const toolCalls: SessionToolCall[] = [...(msg.tool_calls || [])];
  const textParts: string[] = [];
  const thoughtParts: string[] = [];

  let raw: unknown = msg.content;
  if (!raw && msg.text) {
    raw = msg.text;
  }

  // If raw is a string, check if it's JSON encoded
  if (typeof raw === 'string') {
    const trimmed = raw.trim();
    if (
      (trimmed.startsWith('[') && trimmed.endsWith(']')) ||
      (trimmed.startsWith('{') && trimmed.endsWith('}'))
    ) {
      try {
        raw = JSON.parse(trimmed);
      } catch {
        // Not valid JSON, keep as raw string
      }
    }
  }

  const processItem = (item: unknown) => {
    if (typeof item === 'string') {
      const trimmed = item.trim();
      if (trimmed) textParts.push(trimmed);
      return;
    }
    if (typeof item === 'object' && item !== null) {
      const part = item as Record<string, unknown>;

      // Check text content
      if (typeof part.text === 'string' && part.text.trim()) {
        textParts.push(part.text.trim());
      } else if (typeof part.content === 'string' && part.content.trim()) {
        textParts.push(part.content.trim());
      }

      // Check thought / reasoning
      if (typeof part.thought === 'string' && part.thought.trim()) {
        thoughtParts.push(part.thought.trim());
      } else if (typeof part.reasoning === 'string' && part.reasoning.trim()) {
        thoughtParts.push(part.reasoning.trim());
      }

      // Check embedded tool calls
      const isTool =
        part.type === 'tool_use' ||
        part.type === 'tool_call' ||
        part.type === 'tool' ||
        part.name === 'question' ||
        part.name === 'subagent' ||
        typeof part.call === 'string';

      if (isTool) {
        const name = String(part.name || part.call || part.tool || 'tool');
        const stateObj =
          typeof part.state === 'object' && part.state !== null
            ? (part.state as Record<string, unknown>)
            : undefined;

        const input = (stateObj?.input || part.input || part.arguments || part.params) as
          | string
          | Record<string, unknown>
          | undefined;

        let output = (stateObj?.output || part.output || part.result) as
          | string
          | Record<string, unknown>
          | undefined;

        // If output is not directly a string, check state.content
        if (!output && stateObj && Array.isArray(stateObj.content)) {
          const textPieces: string[] = [];
          for (const c of stateObj.content) {
            if (typeof c === 'object' && c !== null && typeof (c as { text?: string }).text === 'string') {
              textPieces.push((c as { text: string }).text);
            }
          }
          if (textPieces.length > 0) {
            output = textPieces.join('\n');
          }
        }

        // Check if there is an error
        const errorObj = stateObj?.error || part.error;
        let errorMessage: string | undefined;
        if (errorObj) {
          if (typeof errorObj === 'string') {
            errorMessage = errorObj;
          } else if (typeof errorObj === 'object' && errorObj !== null) {
            const errRec = errorObj as Record<string, unknown>;
            errorMessage = typeof errRec.message === 'string' ? errRec.message : JSON.stringify(errRec);
          }
        }

        const status = typeof stateObj?.status === 'string'
          ? (stateObj.status as string)
          : typeof part.status === 'string'
          ? (part.status as string)
          : errorMessage
          ? 'error'
          : 'completed';

        const metadata = (stateObj?.metadata || part.metadata) as Record<string, unknown> | undefined;
        const exitCode = typeof metadata?.exit === 'number'
          ? (metadata.exit as number)
          : typeof part.exit_code === 'number'
          ? (part.exit_code as number)
          : (status === 'error' || errorMessage ? 1 : 0);

        toolCalls.push({
          name,
          input,
          output: output || errorMessage,
          status,
          exit_code: exitCode,
          session_id: typeof metadata?.sessionId === 'string' ? metadata.sessionId : undefined,
        });
      }
    }
  };

  if (typeof raw === 'string') {
    textParts.push(raw);
  } else if (Array.isArray(raw)) {
    for (const item of raw) {
      processItem(item);
    }
  } else if (typeof raw === 'object' && raw !== null) {
    const obj = raw as Record<string, unknown>;
    if (Array.isArray(obj.parts)) {
      for (const p of obj.parts) {
        processItem(p);
      }
    } else {
      processItem(obj);
    }
  }

  // Fallback to msg.text if nothing was found
  if (textParts.length === 0 && msg.text && msg.text.trim()) {
    textParts.push(msg.text.trim());
  }

  const fullText = textParts.join('\n\n');
  const extractedQuestions = extractQuestionsFromText(fullText);

  return {
    text: fullText,
    thoughts: thoughtParts.join('\n\n') || undefined,
    toolCalls,
    extractedQuestions: extractedQuestions.length > 0 ? extractedQuestions : undefined,
  };
}
