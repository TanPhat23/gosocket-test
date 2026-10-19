import React from 'react';
import type { TranscriptEntry } from '../../hooks/useSessionTranscript';
import type { ParsedMessageData } from '../../utils/sessionMessage';
import { AssistantMessage } from './AssistantMessage';
import { userPromptText, formatTimestamp } from '../../utils/sessionFormat';
import { SteeringSlot } from './SteeringSlot';
import { UserMessage } from './UserMessage';

interface SessionTranscriptMessagesProps {
  transcript: TranscriptEntry[];
  endRef: React.RefObject<HTMLDivElement | null>;
}

const INTERNAL_NOTICES = ['The Code Mode tool catalog has changed', '<mcp_instructions>'];

// Bookkeeping the agent writes for itself: it is not part of the conversation.
function isInternalInstructionDump({ text, thoughts, toolCalls }: ParsedMessageData): boolean {
  return (
    (INTERNAL_NOTICES.some((n) => text.includes(n)) || text.startsWith('Current thought number')) &&
    toolCalls.length === 0 &&
    !thoughts
  );
}

// Memoized so that typing in the steer box, which re-renders the session view, does not re-render every message.
// Messages take their state from the session contexts, so none of it is passed down through this list.
export const SessionTranscriptMessages = React.memo(function SessionTranscriptMessages({ transcript, endRef }: SessionTranscriptMessagesProps) {
  return (
    <>
      {transcript.map(({ msg, msgIdx, parsed, hasSubsequentUserMessage }) => {
        if (msg.role === 'system' || msg.type === 'system') return null;

        if (msg.role === 'user' || msg.type === 'user') {
          return (
            <UserMessage
              key={msg.id || `user-${msgIdx}`}
              rawText={userPromptText(msg, parsed.text)}
              timeString={formatTimestamp(msg.created_at)}
            />
          );
        }

        const isEmpty = !parsed.text && !parsed.thoughts && parsed.toolCalls.length === 0;
        if (isEmpty || isInternalInstructionDump(parsed)) return null;

        return (
          <AssistantMessage
            key={msg.id || `assistant-${msgIdx}`}
            msg={msg}
            msgIdx={msgIdx}
            parsed={parsed}
            hasSubsequentUserMessage={hasSubsequentUserMessage}
          />
        );
      })}
      <SteeringSlot />
      <div ref={endRef} />
    </>
  );
});
