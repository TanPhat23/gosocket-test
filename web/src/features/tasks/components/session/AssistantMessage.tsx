import { Bot, ChevronDown, Sparkles, Terminal } from 'lucide-react';
import { Markdown } from '@/components/ui/markdown';
import type { SessionMessage } from '../../schemas/taskSchemas';
import type { ParsedMessageData } from '../../utils/sessionMessage';
import { formatTimestamp } from '../../utils/sessionFormat';
import { SessionQuestionnaire } from './SessionQuestionnaire';
import { ToolCallCard } from './ToolCallCard';

interface AssistantMessageProps {
  msg: SessionMessage;
  msgIdx: number;
  parsed: ParsedMessageData;
  // True when a user message follows, so a questionnaire found in the text has already been answered.
  hasSubsequentUserMessage: boolean;
}

// The agent's reply: who wrote it, its reasoning, its text and the tool calls it made.
export function AssistantMessage({ msg, msgIdx, parsed, hasSubsequentUserMessage }: AssistantMessageProps) {
  const { text, thoughts, toolCalls, extractedQuestions } = parsed;
  const timeString = formatTimestamp(msg.created_at);
  const hasQuestionTool = toolCalls.some((tc) => tc.name === 'question');

  return (
    <div className="flex justify-start gap-3" data-testid="assistant-message">
      <div className="mt-0.5 hidden size-7 sm:flex shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary">
        <Bot className="size-4" />
      </div>
      <div className="min-w-0 max-w-full flex-1 sm:max-w-[92%] space-y-2.5 text-sm">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-muted-foreground">
          <span className="font-semibold text-foreground">{msg.agent ? `Agent (${msg.agent})` : 'Assistant'}</span>
          {msg.model && <span className="rounded bg-muted/60 px-1.5 py-px font-mono text-[10px]">{msg.model}</span>}
          {timeString && <span className="tabular-nums text-[10px]">{timeString}</span>}
        </div>

        {thoughts && (
          <details className="group text-xs text-muted-foreground">
            <summary className="flex w-fit cursor-pointer select-none items-center gap-1.5 rounded-full border border-border/60 bg-muted/30 px-2.5 py-1 hover:text-foreground">
              <Sparkles className="size-3 shrink-0 text-purple-400" />
              <span className="font-medium">Thought process</span>
              <ChevronDown className="size-3 transition-transform group-open:rotate-180" />
            </summary>
            <div className="mt-2 whitespace-pre-wrap rounded-md border-l-2 border-purple-500/40 bg-muted/30 p-2.5 font-mono text-[11px] leading-relaxed text-foreground/90">
              {thoughts}
            </div>
          </details>
        )}

        {text && (
          <div className="leading-relaxed text-foreground">
            <Markdown content={text} />
          </div>
        )}

        {!hasQuestionTool && extractedQuestions && extractedQuestions.length > 0 && (
          <div className="pt-2">
            <SessionQuestionnaire questions={extractedQuestions} status={hasSubsequentUserMessage ? 'completed' : 'pending'} />
          </div>
        )}

        {toolCalls.length > 0 && (
          <div className="space-y-1.5">
            <span className="flex items-center gap-1.5 text-[11px] font-medium text-muted-foreground">
              <Terminal className="size-3" /> {toolCalls.length} tool {toolCalls.length === 1 ? 'call' : 'calls'}
            </span>
            <div className="space-y-1">
              {toolCalls.map((tc, tcIdx) => {
                if (tc.name === 'question') {
                  return (
                    <div key={tcIdx} className="pt-1">
                      <SessionQuestionnaire
                        // The question tool's input is the questionnaire the agent asked.
                        questions={tc.input as never}
                        status={tc.status}
                        output={tc.output}
                      />
                    </div>
                  );
                }
                return <ToolCallCard key={tcIdx} tc={tc} expandKey={`${msgIdx}-${tcIdx}`} />;
              })}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
