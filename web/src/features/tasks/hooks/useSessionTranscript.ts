import { useMemo } from 'react';
import type { SessionMessage, SessionToolCall } from '../schemas/taskSchemas';
import { parseSessionMessageContent } from '../utils/sessionMessage';
import type { ParsedMessageData } from '../utils/sessionMessage';

export interface TranscriptEntry {
  msg: SessionMessage;
  msgIdx: number;
  parsed: ParsedMessageData;
  hasSubsequentUserMessage: boolean;
}

export interface IndexedToolCall {
  tc: SessionToolCall;
  msg: SessionMessage;
}

// The messages sorted by time and parsed once per session update, and every tool call keyed `${msgIdx}-${tcIdx}` for
// the inspector. Without this, each render (for example each keystroke in the steer box) re-sorted and re-parsed it all.
export function useSessionTranscript(messages: SessionMessage[] | undefined) {
  const transcript = useMemo<TranscriptEntry[]>(() => {
    const sorted = [...(messages || [])].sort((a, b) => {
      const tA = a.created_at ? new Date(a.created_at).getTime() : 0;
      const tB = b.created_at ? new Date(b.created_at).getTime() : 0;
      return tA - tB;
    });
    let lastUserIdx = -1;
    sorted.forEach((m, i) => {
      if (m.role === 'user' || m.type === 'user') lastUserIdx = i;
    });
    return sorted.map((msg, msgIdx) => ({
      msg,
      msgIdx,
      parsed: parseSessionMessageContent(msg),
      // True when a user message comes after this one in the transcript.
      hasSubsequentUserMessage: msgIdx < lastUserIdx,
    }));
  }, [messages]);

  const toolCalls = useMemo(() => {
    const byKey = new Map<string, IndexedToolCall>();
    for (const { msg, msgIdx, parsed } of transcript) {
      parsed.toolCalls.forEach((tc, tcIdx) => byKey.set(`${msgIdx}-${tcIdx}`, { tc, msg }));
    }
    return byKey;
  }, [transcript]);

  return { transcript, toolCalls };
}
