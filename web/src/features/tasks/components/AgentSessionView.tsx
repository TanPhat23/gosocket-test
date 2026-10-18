import React, { useState, useEffect, useRef, useCallback, useLayoutEffect } from 'react';
import { taskApi } from '../api/taskApi';
import { useTaskStore } from '../store/useTaskStore';
import { parseWsFrames } from '../utils/terminalParser';
import type { TaskSessionResponse, SessionMessage, SessionToolCall } from '../schemas/taskSchemas';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';
import {
  Copy,
  Check,
  RefreshCw,
  Zap,
  Send,
  AlertTriangle,
  AlertCircle,
  Eye,
  Terminal,
  Bot,
  User,
  CheckCircle2,
  XCircle,
  Clock,
  Sparkles,
  ChevronDown,
  ChevronRight,
  Database,
  Settings2,
  FileCode,
  X,
  CornerDownLeft,
  ArrowDown,
  Info,
  Square,
} from 'lucide-react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { taskSessionQueryOptions } from '../queries';
import { AgentQuestionnaire, type QuestionItemData } from './AgentQuestionnaire';
import { extractQuestionsFromText } from '../utils/questionParser';
import { cleanUserPrompt } from '../utils/promptCleaner';
import { Markdown } from '@/components/ui/markdown';

interface AgentSessionViewProps {
  taskId: string;
  defaultSessionId?: string | null;
  isActive?: boolean;
}

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

const formatTimestamp = (ts?: number | string | null) => {
  if (!ts) return null;
  try {
    const date = typeof ts === 'number' ? new Date(ts) : new Date(ts);
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  } catch {
    return null;
  }
};

interface TranscriptEntry {
  msg: SessionMessage;
  msgIdx: number;
  parsed: ParsedMessageData;
  hasSubsequentUserMessage: boolean;
}

interface SessionTranscriptMessagesProps {
  transcript: TranscriptEntry[];
  expandedTools: Record<string, boolean>;
  selectedToolKey: string | null;
  isSteeringActive: boolean;
  isSubmittingPrompt: boolean;
  toggleToolExpanded: (key: string) => void;
  selectTool: (key: string, openInputTab: boolean) => void;
  submitAnswer: (answerText: string) => Promise<void>;
  endRef: React.RefObject<HTMLDivElement | null>;
}

// Memoized so that typing in the steer box, which re-renders AgentSessionView, does not re-render every message.
const SessionTranscriptMessages = React.memo(function SessionTranscriptMessages({
  transcript,
  expandedTools,
  selectedToolKey,
  isSteeringActive,
  isSubmittingPrompt,
  toggleToolExpanded,
  selectTool,
  submitAnswer,
  endRef,
}: SessionTranscriptMessagesProps) {
  return (
    <>
      {transcript.map(({ msg, msgIdx, parsed, hasSubsequentUserMessage }) => {
        const isUser = msg.role === 'user' || msg.type === 'user';
        const isSystem = msg.role === 'system' || msg.type === 'system';
        if (isSystem) {
          return null;
        }

        const { text: textContent, thoughts, toolCalls, extractedQuestions } = parsed;
        const timeString = formatTimestamp(msg.created_at);
        const hasQuestionTool = toolCalls.some((tc) => tc.name === 'question');

        // Skip rendering empty bubbles or internal tool catalog instruction notices
        const isInternalInstructionDump =
          !isUser &&
          (textContent.includes('The Code Mode tool catalog has changed') ||
            textContent.includes('<mcp_instructions>') ||
            textContent.startsWith('Current thought number')) &&
          toolCalls.length === 0 &&
          !thoughts;

        if (!isUser && ((!textContent && !thoughts && toolCalls.length === 0) || isInternalInstructionDump)) {
          return null;
        }

        if (isUser) {
          // Ensure text is extracted even if in msg.text, msg.content, or JSON formatted
          let rawUserText = textContent || (typeof msg.text === 'string' ? msg.text : '') || (typeof msg.content === 'string' ? msg.content : '');
          if (!rawUserText && typeof msg.content === 'object' && msg.content !== null) {
            const cObj = msg.content as Record<string, unknown>;
            if (typeof cObj.text === 'string') rawUserText = cObj.text;
          }
          let cleanUserText = rawUserText.trim();
          if (cleanUserText.startsWith('"') && cleanUserText.endsWith('"') && cleanUserText.length >= 2) {
            try {
              cleanUserText = JSON.parse(cleanUserText);
            } catch {
              cleanUserText = cleanUserText.slice(1, -1);
            }
          }
          if (!cleanUserText) {
            cleanUserText = '(User Prompt)';
          }

          const { prompt: displayPrompt, systemContext } = cleanUserPrompt(cleanUserText);

          return (
            <div
              key={msg.id || `user-${msgIdx}`}
              className="flex justify-end"
              data-testid="user-message"
            >
              <div className="max-w-[85%] rounded-xl p-3.5 bg-muted/60 border border-border/80 text-foreground text-xs space-y-2 shadow-sm">
                <div className="flex items-center justify-between gap-3 text-[10px] text-muted-foreground border-b border-border/60 pb-1.5">
                  <span className="font-semibold flex items-center gap-1.5 text-foreground">
                    <User className="size-3 text-primary" /> User Prompt
                  </span>
                  {timeString && <span className="font-mono">{timeString}</span>}
                </div>
                <div className="leading-relaxed font-sans text-xs">
                  <Markdown content={displayPrompt} />
                </div>

                {systemContext && (
                  <details className="mt-2 rounded-lg bg-background/80 border border-border/60 p-2 text-foreground/90 font-mono text-[11px] select-none group">
                    <summary className="cursor-pointer font-semibold text-[10px] uppercase tracking-wider flex items-center justify-between select-none text-muted-foreground hover:text-foreground">
                      <span className="flex items-center gap-1.5">
                        <Settings2 className="size-3 text-primary" /> System Context &amp; MCP Tool Catalog
                      </span>
                      <span className="text-[9px] opacity-75 font-normal">
                        {systemContext.split('\n').length} lines
                      </span>
                    </summary>
                    <pre className="mt-2 max-h-52 overflow-y-auto whitespace-pre-wrap p-2 bg-muted/40 rounded border border-border/40 text-[10px] leading-relaxed text-foreground/90 font-mono">
                      {systemContext}
                    </pre>
                  </details>
                )}
              </div>
            </div>
          );
        }

        // Assistant Message
        return (
          <div
            key={msg.id || `assistant-${msgIdx}`}
            className="flex justify-start"
            data-testid="assistant-message"
          >
            <div className="max-w-[90%] w-full rounded-lg p-3.5 bg-muted/30 border border-border text-xs space-y-3">
              {/* Header info */}
              <div className="flex items-center justify-between gap-2 text-[10px] text-muted-foreground border-b border-border/60 pb-1.5">
                <div className="flex items-center gap-1.5 font-medium">
                  <Bot className="size-3 text-primary" />
                  <span className="font-semibold text-foreground">
                    {msg.agent ? `Agent (${msg.agent})` : 'Assistant'}
                  </span>
                  {msg.model && (
                    <span className="font-mono opacity-80">[{msg.model}]</span>
                  )}
                </div>
                {timeString && <span className="font-mono">{timeString}</span>}
              </div>

              {/* Agent Reasoning / Thoughts (if present) */}
              {thoughts && (
                <details className="text-[11px] text-muted-foreground bg-muted/50 rounded-lg p-2.5 border border-border/60 group">
                  <summary className="cursor-pointer font-medium flex items-center justify-between select-none text-muted-foreground hover:text-foreground">
                    <span className="flex items-center gap-1.5">
                      <Sparkles className="size-3.5 text-purple-400 shrink-0" />
                      <span className="font-semibold text-foreground/90">Thought Process</span>
                    </span>
                    <span className="text-[10px] text-muted-foreground opacity-70 group-open:rotate-180 transition-transform">▼</span>
                  </summary>
                  <div className="mt-2.5 whitespace-pre-wrap font-mono text-[10.5px] leading-relaxed text-foreground/90 pl-3 border-l-2 border-purple-500/40 bg-background/50 p-2 rounded">
                    {thoughts}
                  </div>
                </details>
              )}

              {/* Text reply rendered with Markdown */}
              {textContent && (
                <div className="leading-relaxed text-foreground font-sans text-xs">
                  <Markdown content={textContent} />
                </div>
              )}

              {/* Extracted Questionnaire from message text (if no explicit question tool call) */}
              {!hasQuestionTool && extractedQuestions && extractedQuestions.length > 0 && (
                <div className="pt-2">
                  <AgentQuestionnaire
                    questions={extractedQuestions}
                    status={hasSubsequentUserMessage ? 'completed' : 'pending'}
                    onSubmitAnswer={submitAnswer}
                    isSubmitting={isSubmittingPrompt}
                  />
                </div>
              )}

              {/* Tool executions */}
              {toolCalls.length > 0 && (
                <div className="space-y-2 pt-1">
                  <span className="text-[10px] uppercase font-semibold text-muted-foreground tracking-wider flex items-center gap-1">
                    <Terminal className="size-3" /> Tool Executions ({toolCalls.length})
                  </span>
                  <div className="space-y-1.5">
                    {toolCalls.map((tc: SessionToolCall, tcIdx: number) => {
                      if (tc.name === 'question') {
                        return (
                          <div key={tcIdx} className="pt-1">
                            <AgentQuestionnaire
                              questions={tc.input as any}
                              status={tc.status}
                              output={tc.output}
                              onSubmitAnswer={submitAnswer}
                              isSubmitting={isSubmittingPrompt}
                            />
                          </div>
                        );
                      }

                      const expandKey = `${msgIdx}-${tcIdx}`;
                      const isExpanded = !!expandedTools[expandKey];
                      const isSelectedInInspector = selectedToolKey === expandKey;

                      const isSubagent = tc.name === 'subagent';
                      const inputObj =
                        typeof tc.input === 'object' && tc.input !== null
                          ? (tc.input as Record<string, unknown>)
                          : null;
                      const subagentAgent = inputObj ? String(inputObj.agent || '') : '';
                      const subagentDesc = inputObj
                        ? String(inputObj.description || inputObj.prompt || '')
                        : '';

                      const inputStr =
                        typeof tc.input === 'string'
                          ? tc.input
                          : tc.input
                          ? JSON.stringify(tc.input, null, 2)
                          : '';

                      const outputStr =
                        typeof tc.output === 'string'
                          ? tc.output
                          : tc.output
                          ? JSON.stringify(tc.output, null, 2)
                          : '';

                      const exitCode = tc.exit_code;
                      const isSuccess =
                        tc.status !== 'error' && (exitCode === undefined || exitCode === 0);

                      return (
                        <div
                          key={tcIdx}
                          onClick={() => selectTool(expandKey, isSubagent && !tc.output)}
                          className={`rounded-lg border transition-all overflow-hidden font-mono text-[11px] cursor-pointer ${
                            isSelectedInInspector
                              ? 'border-cyan-500/80 ring-1 ring-cyan-500/40 bg-cyan-950/10'
                              : !isSuccess
                              ? 'border-rose-500/50 bg-rose-950/10'
                              : 'border-border/80 bg-background/80'
                          }`}
                          data-testid="tool-call-card"
                        >
                          <div
                            className={`w-full flex items-center justify-between p-2.5 transition-colors text-left select-none ${
                              isSelectedInInspector
                                ? 'bg-cyan-500/10'
                                : !isSuccess
                                ? 'bg-rose-500/10 hover:bg-rose-500/15'
                                : 'bg-muted/40 hover:bg-muted/60'
                            }`}
                          >
                            <div className="flex items-center gap-2 min-w-0 flex-1">
                              <button
                                type="button"
                                onClick={(e) => {
                                  e.stopPropagation();
                                  toggleToolExpanded(expandKey);
                                }}
                                className="p-0.5 hover:bg-muted/80 rounded"
                                title={isExpanded ? 'Collapse inline preview' : 'Expand inline preview'}
                                data-testid={`toggle-tool-${tc.name}`}
                              >
                                {isExpanded ? (
                                  <ChevronDown className="size-3 text-muted-foreground shrink-0" />
                                ) : (
                                  <ChevronRight className="size-3 text-muted-foreground shrink-0" />
                                )}
                              </button>
                              <Badge
                                variant="outline"
                                className={`font-mono text-[10px] px-1.5 py-0 shrink-0 ${
                                  isSubagent
                                    ? 'bg-purple-500/10 text-purple-400 border-purple-500/30'
                                    : 'bg-primary/10 text-primary border-primary/20'
                                }`}
                              >
                                {tc.name}
                              </Badge>
                              {isSubagent && subagentAgent && (
                                <Badge
                                  variant="outline"
                                  className="font-mono text-[9px] px-1 py-0 bg-primary/10 text-primary border-primary/20 shrink-0"
                                >
                                  {subagentAgent}
                                </Badge>
                              )}
                              <span className="truncate text-muted-foreground min-w-0 flex-1">
                                {isSubagent && subagentDesc
                                  ? subagentDesc
                                  : inputStr.replace(/\n/g, ' ')}
                              </span>
                            </div>

                            <div className="flex items-center gap-2 shrink-0">
                              {isSelectedInInspector && (
                                <span className="text-[10px] font-sans font-semibold text-cyan-600 dark:text-cyan-400 hidden sm:inline-flex items-center gap-1">
                                  <Eye className="size-3 shrink-0" />
                                  <span>Inspecting in Right Pane</span>
                                </span>
                              )}
                              {tc.status === 'running' ? (
                                <Badge
                                  variant="outline"
                                  className="font-mono text-[9px] px-1.5 py-0 text-sky-500 border-sky-500/30 flex items-center gap-1"
                                >
                                  <span className="size-1.5 rounded-full bg-sky-500 animate-pulse" />
                                  running
                                </Badge>
                              ) : !isSuccess ? (
                                <Badge
                                  variant="outline"
                                  className="font-mono text-[9px] px-1.5 py-0 bg-rose-500/15 text-rose-600 dark:text-rose-400 border-rose-500/40"
                                >
                                  {exitCode !== undefined && exitCode !== 0
                                    ? `exit ${exitCode}`
                                    : 'exit 1'}
                                </Badge>
                              ) : (
                                <Badge
                                  variant="outline"
                                  className="font-mono text-[9px] px-1.5 py-0 bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border-emerald-500/30"
                                >
                                  exit {exitCode ?? 0}
                                </Badge>
                              )}
                            </div>
                          </div>

                          {isExpanded && (
                            <div className="p-2.5 border-t border-border/60 space-y-2 bg-background font-mono text-[11px]">
                              {isSubagent && inputObj && (
                                <div className="rounded p-2 bg-purple-500/5 border border-purple-500/15 space-y-1 text-xs font-sans">
                                  <div className="flex items-center gap-2">
                                    <span className="font-semibold text-purple-400">Subagent:</span>
                                    <span className="font-mono text-[11px] text-foreground">
                                      {subagentAgent ? `[${subagentAgent}] ` : ''}
                                      {subagentDesc}
                                    </span>
                                  </div>
                                </div>
                              )}

                              {inputStr && (
                                <div className="space-y-1">
                                  <div className="text-[10px] text-muted-foreground uppercase font-semibold">
                                    {isSubagent ? 'Prompt / Task Brief' : 'Input'}
                                  </div>
                                  {isSubagent && inputObj?.prompt ? (
                                    <div className="p-2.5 rounded bg-muted/40 border border-border/50 text-[11px] overflow-x-auto text-foreground max-h-56 font-sans">
                                      <Markdown content={String(inputObj.prompt)} />
                                    </div>
                                  ) : (
                                    <pre className="p-2 rounded bg-muted/50 text-[10px] overflow-x-auto whitespace-pre-wrap text-foreground max-h-48 font-mono">
                                      {inputStr}
                                    </pre>
                                  )}
                                </div>
                              )}

                              <div className="space-y-1">
                                <div className="text-[10px] text-muted-foreground uppercase font-semibold">
                                  {isSubagent ? 'Subagent Output / Results' : 'Output'}
                                </div>
                                {tc.status === 'running' ? (
                                  <div className="flex items-center gap-2 p-3 rounded bg-muted/30 border border-border/50 text-xs text-primary font-mono">
                                    <RefreshCw className="size-3.5 animate-spin text-primary" />
                                    <span>
                                      {isSubagent
                                        ? 'Subagent is executing in child session...'
                                        : 'Tool execution in progress...'}
                                    </span>
                                  </div>
                                ) : outputStr ? (
                                  <pre
                                    className={`p-2 rounded text-[10px] overflow-x-auto whitespace-pre-wrap max-h-60 font-mono ${
                                      tc.status === 'error' || !isSuccess
                                        ? 'bg-destructive/10 text-destructive border border-destructive/20'
                                        : 'bg-muted/50 text-foreground'
                                    }`}
                                  >
                                    {outputStr}
                                  </pre>
                                ) : (
                                  <div className="text-[10px] text-muted-foreground italic p-1">
                                    No output recorded.
                                  </div>
                                )}
                              </div>
                            </div>
                          )}
                        </div>
                      );
                    })}
                  </div>
                </div>
              )}
            </div>
          </div>
        );
      })}
      {isSteeringActive && (
        <div
          className="flex justify-start animate-fade-in"
          data-testid="steering-live-indicator"
        >
          <div className="max-w-[90%] w-full rounded-lg p-3 bg-muted/40 border border-primary/30 text-xs space-y-2 shadow-sm">
            <div className="flex items-center justify-between text-[11px] font-mono text-primary">
              <span className="flex items-center gap-1.5 font-semibold">
                <Bot className="size-3.5 animate-pulse" /> Agent Streaming &amp; Reasoning...
              </span>
              <span className="flex items-center gap-1 text-[10px] text-muted-foreground">
                <RefreshCw className="size-2.5 animate-spin text-primary" /> Live
              </span>
            </div>
            <div className="flex items-center gap-2 text-muted-foreground">
              <span className="size-1.5 rounded-full bg-primary animate-ping" />
              <span className="text-[11px] font-mono">
                Generating tool calls and response...
              </span>
            </div>
          </div>
        </div>
      )}
      <div ref={endRef} />
    </>
  );
});

function sessionErrorMessage(err: { status?: number; message?: string }): string {
  if (err.status === 503) return 'Task execution is temporarily disabled by the server.';
  if (err.status === 404) return 'No active session initialized for this task yet.';
  return err.message || 'Failed to load task session.';
}

export const AgentSessionView: React.FC<AgentSessionViewProps> = ({
  taskId,
  defaultSessionId,
  isActive = true,
}) => {
  // The session lives in the shared task-session query, so the inspector's metrics and this transcript use one request.
  // Stream frames and optimistic messages write into the same cache entry.
  const queryClient = useQueryClient();
  const sessionOptions = taskSessionQueryOptions(taskId);
  // retry: false keeps the immediate error banner. refetchOnMount: false lets this tab reuse the inspector's fetch.
  const sessionQuery = useQuery({ ...sessionOptions, retry: false, refetchOnMount: false });
  const session = sessionQuery.data ?? null;
  const sessionError = sessionQuery.error as { status?: number; message?: string } | null;
  const isUnavailable = sessionError?.status === 503;
  const error = sessionError ? sessionErrorMessage(sessionError) : null;
  const loading = sessionQuery.isFetching && !session;
  const isSimulated = session?.simulated === true;
  // Mirrors streamLive for the terminal socket handler, which is set up once per task.
  const streamLiveRef = useRef(false);
  const [copiedSessionId, setCopiedSessionId] = useState<boolean>(false);

  // Steer prompt state
  const [steerPrompt, setSteerPrompt] = useState<string>('');
  const [isSubmittingPrompt, setIsSubmittingPrompt] = useState<boolean>(false);
  const [isSteeringActive, setIsSteeringActive] = useState<boolean>(false);
  const [steerError, setSteerError] = useState<string | null>(null);

  // Compact state
  const [isCompacting, setIsCompacting] = useState<boolean>(false);
  const [isInterrupting, setIsInterrupting] = useState<boolean>(false);
  const [compactMessage, setCompactMessage] = useState<string | null>(null);

  // View toggles for Inspector tabs
  const [inspectorInputView, setInspectorInputView] = useState<'markdown' | 'raw'>('markdown');
  const [inspectorOutputView, setInspectorOutputView] = useState<'markdown' | 'raw'>('markdown');

  // Selected tool key for inspector: `${msgIdx}-${tcIdx}`
  const [selectedToolKey, setSelectedToolKey] = useState<string | null>(null);
  const [inspectorTab, setInspectorTab] = useState<'output' | 'input'>('output');

  // Expanded tool calls tracker: msgIndex-toolIndex
  const [expandedTools, setExpandedTools] = useState<Record<string, boolean>>({});

  // Stable callbacks: SessionTranscriptMessages is memoized and must not re-render when the steer draft changes.
  const toggleToolExpanded = useCallback((key: string) => {
    setExpandedTools((prev) => ({
      ...prev,
      [key]: !prev[key],
    }));
  }, []);

  const selectTool = useCallback((key: string, openInputTab: boolean) => {
    setSelectedToolKey(key);
    if (openInputTab) {
      setInspectorTab('input');
    }
    setInspectorInputView('markdown');
    setInspectorOutputView('markdown');
  }, []);

  // The transcript is sorted by time and each message is parsed once per session update. Without this, every render
  // (for example each keystroke in the steer box) re-sorted and re-parsed the whole transcript.
  const transcript = React.useMemo(() => {
    const sortedMsgs = [...(session?.messages || [])].sort((a, b) => {
      const tA = a.created_at ? new Date(a.created_at).getTime() : 0;
      const tB = b.created_at ? new Date(b.created_at).getTime() : 0;
      return tA - tB;
    });
    let lastUserIdx = -1;
    sortedMsgs.forEach((m, i) => {
      if (m.role === 'user' || m.type === 'user') lastUserIdx = i;
    });
    return sortedMsgs.map((msg, msgIdx) => ({
      msg,
      msgIdx,
      parsed: parseSessionMessageContent(msg),
      // True when a user message comes after this one in the transcript.
      hasSubsequentUserMessage: msgIdx < lastUserIdx,
    }));
  }, [session?.messages]);

  // Helper to collect all tool calls across messages for inspector lookup and metrics
  const allToolCallsMap = React.useMemo(() => {
    const map = new Map<
      string,
      {
        tc: SessionToolCall;
        msgIdx: number;
        tcIdx: number;
        msg: SessionMessage;
      }
    >();
    for (const { msg, msgIdx, parsed } of transcript) {
      parsed.toolCalls.forEach((tc, tIdx) => {
        map.set(`${msgIdx}-${tIdx}`, { tc, msgIdx, tcIdx: tIdx, msg });
      });
    }
    return map;
  }, [transcript]);

  const selectedToolInfo = selectedToolKey ? allToolCallsMap.get(selectedToolKey) : null;

  const transcriptEndRef = useRef<HTMLDivElement>(null);
  const transcriptContainerRef = useRef<HTMLDivElement>(null);
  const [showScrollBottomBtn, setShowScrollBottomBtn] = useState<boolean>(false);
  const prevMessagesCountRef = useRef<number>(0);

  const scrollToBottom = (smooth = true) => {
    const el = transcriptContainerRef.current;
    if (el) {
      try {
        if (typeof el.scrollTo === 'function') {
          el.scrollTo({ top: el.scrollHeight, behavior: smooth ? 'smooth' : 'auto' });
        } else {
          el.scrollTop = el.scrollHeight;
        }
      } catch {
        // Fallback if environment (e.g. mock DOM where scrollTop is non-writable property) prevents direct assignment
      }
    }
    transcriptEndRef.current?.scrollIntoView({ behavior: smooth ? 'smooth' : 'auto' });
  };

  const handleTranscriptScroll = () => {
    const el = transcriptContainerRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    // Show button when scrolled up by more than 100px
    setShowScrollBottomBtn(distanceFromBottom > 100);
  };

  // Refreshes the shared session query. Overlapping refreshes (polling, the terminal stream, prompts) share one request.
  const refreshSession = () =>
    queryClient.fetchQuery({ ...sessionOptions, staleTime: 0, retry: false }).catch(() => undefined);

  // When isActive becomes true, trigger scrollToBottom with delays (50ms and 150ms) to ensure layout paints
  useEffect(() => {
    if (isActive) {
      const t1 = setTimeout(() => {
        scrollToBottom(false);
      }, 50);
      const t2 = setTimeout(() => {
        scrollToBottom(false);
      }, 150);
      return () => {
        clearTimeout(t1);
        clearTimeout(t2);
      };
    }
  }, [isActive]);

  // Auto-scroll chat to bottom on initial load and whenever messages arrive
  useEffect(() => {
    const currentCount = session?.messages?.length ?? 0;
    if (currentCount > 0) {
      if (prevMessagesCountRef.current === 0) {
        // Initial load scroll
        const t1 = setTimeout(() => scrollToBottom(false), 50);
        return () => clearTimeout(t1);
      } else if (currentCount > prevMessagesCountRef.current) {
        // New messages arrived
        const t2 = setTimeout(() => scrollToBottom(true), 50);
        return () => clearTimeout(t2);
      }
    }
    prevMessagesCountRef.current = currentCount;
  }, [session?.messages]);

  // Live session stream: the server relays the (polled) OpenCode session over a WebSocket and pushes only
  // changes. While it is connected the browser does not poll; if it drops we fall back to polling below.
  const [streamLive, setStreamLive] = useState(false);
  useEffect(() => {
    if (!taskId || typeof WebSocket === 'undefined') return;
    let socket: WebSocket | null = null;
    try {
      const isHttps = typeof window !== 'undefined' && window.location.protocol === 'https:';
      const host = typeof window !== 'undefined' ? window.location.host : 'localhost:8080';
      socket = new WebSocket(`${isHttps ? 'wss:' : 'ws:'}//${host}/api/v1/ws/session/${encodeURIComponent(taskId)}`);
      socket.onmessage = (event) => {
        try {
          const msg = JSON.parse(typeof event.data === 'string' ? event.data : '') as {
            type?: string;
            session?: TaskSessionResponse;
          };
          if (msg.type === 'session' && msg.session) {
            streamLiveRef.current = true;
            setStreamLive(true);
            // A written frame also clears an earlier error, as a successful query fetch does.
            queryClient.setQueryData(sessionOptions.queryKey, msg.session);
            if (msg.session.status && msg.session.status !== 'running') setIsSteeringActive(false);
          } else if (msg.type === 'error') {
            // The relay could not read the session (e.g. sandbox not running): use the normal fetch path.
            streamLiveRef.current = false;
            setStreamLive(false);
          }
        } catch {
          // ignore malformed frames
        }
      };
      socket.onclose = () => {
        streamLiveRef.current = false;
        setStreamLive(false);
      };
      socket.onerror = () => {
        streamLiveRef.current = false;
        setStreamLive(false);
      };
    } catch {
      // streaming is optional; polling covers it
    }
    return () => {
      streamLiveRef.current = false;
      setStreamLive(false);
      socket?.close();
    };
  }, [taskId]);

  // Adaptive auto-refresh when session is in active running state or user is steering (fallback when the
  // live stream is not connected)
  useEffect(() => {
    const isRunning = session?.status === 'running' || isSteeringActive;
    if (!isRunning || streamLive) return;

    let pollCount = 0;
    const maxPolls = 60; // 60 * 800ms = 48s max
    const interval = setInterval(async () => {
      pollCount++;
      await refreshSession();
      transcriptEndRef.current?.scrollIntoView({ behavior: 'smooth' });

      // Stop active steering mode if session outcome is not running after a few checks
      if (session?.status && session.status !== 'running' && pollCount > 4) {
        setIsSteeringActive(false);
      }
      if (pollCount >= maxPolls) {
        setIsSteeringActive(false);
      }
    }, 800);

    return () => clearInterval(interval);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session?.status, isSteeringActive, taskId, streamLive]);

  // Listen for real-time agent_event frames over WebSocket for instant transcript updates
  useEffect(() => {
    if (!taskId) return;
    try {
      const isHttps = typeof window !== 'undefined' && window.location.protocol === 'https:';
      const wsProtocol = isHttps ? 'wss:' : 'ws:';
      const host = typeof window !== 'undefined' ? window.location.host : 'localhost:8080';
      const wsUrl = `${wsProtocol}//${host}/api/v1/ws/terminal/${encodeURIComponent(taskId)}`;
      const socket = new WebSocket(wsUrl);

      socket.onmessage = (event) => {
        const rawData = typeof event.data === 'string' ? event.data : '';
        const frames = parseWsFrames(rawData);
        for (const frame of frames) {
          if (frame.type === 'agent_event') {
            // While the session stream is live it pushes every change, so the terminal event refreshes only without it.
            if (!streamLiveRef.current) void refreshSession();
            const payload = frame.payload as { eventType?: string } | undefined;
            if (payload?.eventType === 'step_finish') {
              setIsSteeringActive(false);
            }
          }
        }
      };

      return () => {
        socket.close();
      };
    } catch {
      // ignore
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [taskId]);

  const handleCopySessionId = async (idToCopy: string) => {
    try {
      await navigator.clipboard.writeText(idToCopy);
      setCopiedSessionId(true);
      setTimeout(() => setCopiedSessionId(false), 2000);
    } catch {
      // ignore clipboard error
    }
  };

  const handleSendPrompt = async (e?: React.FormEvent, customPrompt?: string) => {
    if (e) e.preventDefault();
    const textToSend = (customPrompt !== undefined ? customPrompt : steerPrompt).trim();
    if (!textToSend || isSubmittingPrompt) return;

    const userPromptText = textToSend;
    setIsSubmittingPrompt(true);
    setSteerError(null);
    if (customPrompt === undefined) {
      setSteerPrompt('');
    }
    setIsSteeringActive(true);

    // Optimistically inject user prompt so it displays immediately
    const optimisticMsg: SessionMessage = {
      id: `opt-user-${Date.now()}`,
      type: 'user',
      role: 'user',
      text: userPromptText,
      created_at: new Date().toISOString(),
    };
    queryClient.setQueryData<TaskSessionResponse>(sessionOptions.queryKey, (prev) => {
      if (!prev) return prev;
      return {
        ...prev,
        messages: [...(prev.messages || []), optimisticMsg],
      };
    });

    // Move task to in_progress so it appears in working lane immediately
    useTaskStore.getState().updateTaskStatus(taskId, 'in_progress');

    setTimeout(() => {
      transcriptEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    }, 50);

    try {
      await taskApi.promptTaskSession(taskId, userPromptText);
      // Immediately refresh transcript
      await refreshSession();
      setTimeout(() => {
        transcriptEndRef.current?.scrollIntoView({ behavior: 'smooth' });
      }, 100);
    } catch (err: unknown) {
      const apiErr = err as { message?: string };
      setSteerError(apiErr?.message || 'Failed to deliver steer prompt.');
      setIsSteeringActive(false);
    } finally {
      setIsSubmittingPrompt(false);
    }
  };

  // Questionnaire answers call the latest handleSendPrompt through a ref so submitAnswer keeps one identity.
  const sendPromptRef = useRef(handleSendPrompt);
  useLayoutEffect(() => {
    sendPromptRef.current = handleSendPrompt;
  });
  const submitAnswer = useCallback(async (answerText: string) => {
    await sendPromptRef.current(undefined, answerText);
  }, []);

  const handleInterruptSession = async () => {
    setIsInterrupting(true);
    setCompactMessage(null);
    try {
      await taskApi.interruptTaskSession(taskId);
      setCompactMessage('Interrupt sent. The current turn is stopping.');
      await refreshSession();
      setTimeout(() => setCompactMessage(null), 4000);
    } catch (err: unknown) {
      const apiErr = err as { message?: string };
      setCompactMessage(`Interrupt failed: ${apiErr?.message || 'Unknown error'}`);
    } finally {
      setIsInterrupting(false);
    }
  };

  const handleCompactSession = async () => {
    setIsCompacting(true);
    setCompactMessage(null);
    try {
      await taskApi.compactTaskSession(taskId);
      setCompactMessage('Session context successfully compacted.');
      await refreshSession();
      setTimeout(() => setCompactMessage(null), 4000);
    } catch (err: unknown) {
      const apiErr = err as { message?: string };
      setCompactMessage(`Compaction failed: ${apiErr?.message || 'Unknown error'}`);
    } finally {
      setIsCompacting(false);
    }
  };

  const currentSessionId = session?.session_id || defaultSessionId || 'Unavailable';

  // Status badge styling
  const renderStatusBadge = (status?: string) => {
    const s = (status || 'running').toLowerCase();
    if (s === 'succeeded' || s === 'completed' || s === 'done') {
      return (
        <Badge className="bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border-emerald-500/30 text-[10px] font-mono">
          <CheckCircle2 className="size-3 mr-1 inline" />
          {s}
        </Badge>
      );
    }
    if (s === 'failed' || s === 'error') {
      return (
        <Badge variant="destructive" className="text-[10px] font-mono">
          <XCircle className="size-3 mr-1 inline" />
          {s}
        </Badge>
      );
    }
    if (s === 'idle') {
      return (
        <Badge variant="outline" className="text-muted-foreground text-[10px] font-mono">
          <Clock className="size-3 mr-1 inline" />
          idle
        </Badge>
      );
    }
    return (
      <Badge className="bg-blue-500/15 text-blue-600 dark:text-blue-400 border-blue-500/30 text-[10px] font-mono animate-pulse">
        <Sparkles className="size-3 mr-1 inline" />
        {s}
      </Badge>
    );
  };

  return (
    <div className="flex flex-col h-full overflow-hidden bg-background text-foreground space-y-2.5">
      {/* Minimal Collapsible Session Info Header */}
      <div className="rounded-lg border border-border bg-card shrink-0 text-xs">
        <details className="group">
          <summary className="flex items-center justify-between p-2.5 cursor-pointer select-none hover:bg-muted/30 transition-colors rounded-lg">
            <div className="flex items-center gap-2 min-w-0">
              <Bot className="size-3.5 text-primary shrink-0" />
              <span className="font-semibold text-foreground text-xs">Session Info</span>
              <span className="text-muted-foreground hidden sm:inline">&bull;</span>
              <Badge
                variant="secondary"
                className="font-mono text-[11px] truncate max-w-[160px] sm:max-w-xs select-all bg-muted/60"
                title={currentSessionId}
                data-testid="session-id-display"
                onClick={(e) => e.stopPropagation()}
              >
                {currentSessionId}
              </Badge>
              {!isUnavailable && !isSimulated && renderStatusBadge(session?.status)}
            </div>

            <div className="flex items-center gap-2">
              {/* Quick Compact & Refresh in toolbar */}
              <div className="flex items-center gap-1.5" onClick={(e) => e.stopPropagation()}>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={loading || isCompacting || isUnavailable || isSimulated}
                  onClick={handleCompactSession}
                  className="h-6 px-2 text-[11px] font-mono gap-1 text-muted-foreground hover:text-foreground"
                  title="Compact session context"
                  data-testid="compact-session-btn"
                >
                  <Zap className={`size-3 ${isCompacting ? 'animate-spin text-amber-500' : 'text-amber-500'}`} />
                  <span className="hidden sm:inline">{isCompacting ? 'Compacting...' : 'Compact'}</span>
                </Button>

                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={loading || isInterrupting || isUnavailable || isSimulated}
                  onClick={handleInterruptSession}
                  className="h-6 px-2 text-[11px] font-mono gap-1 text-muted-foreground hover:text-foreground"
                  title="Interrupt the running turn (the task is not cancelled)"
                  data-testid="interrupt-session-btn"
                >
                  <Square className="size-3 text-red-500" />
                  <span className="hidden sm:inline">{isInterrupting ? 'Interrupting...' : 'Interrupt'}</span>
                </Button>

                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={loading}
                  onClick={() => void refreshSession()}
                  className="h-6 px-2 text-[11px] font-mono gap-1 text-muted-foreground hover:text-foreground"
                  title="Refresh transcript"
                  data-testid="refresh-session-btn"
                >
                  <RefreshCw className={`size-3 ${loading ? 'animate-spin' : ''}`} />
                  <span className="hidden sm:inline">Refresh</span>
                </Button>
              </div>

              <ChevronDown className="size-3.5 text-muted-foreground transition-transform duration-200 group-open:rotate-180 shrink-0" />
            </div>
          </summary>

          {/* Collapsible Details: Copy ID, Token metrics row, Model, Compact status */}
          <div className="p-3 pt-0 border-t border-border/60 space-y-2 mt-1">
            <div className="flex flex-wrap items-center justify-between gap-2 pt-2">
              <div className="flex items-center gap-2">
                <span className="text-muted-foreground text-[11px] uppercase tracking-wider font-semibold">
                  Model:
                </span>
                <span className="font-mono text-xs">{session?.model || 'default'}</span>
                {currentSessionId !== 'Unavailable' && (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => handleCopySessionId(currentSessionId)}
                    className="h-6 px-2 text-[11px] font-mono gap-1 text-muted-foreground hover:text-foreground ml-2"
                    data-testid="copy-session-id-btn"
                  >
                    {copiedSessionId ? (
                      <>
                        <Check className="size-3 text-emerald-500" />
                        <span>Copied</span>
                      </>
                    ) : (
                      <>
                        <Copy className="size-3" />
                        <span>Copy ID</span>
                      </>
                    )}
                  </Button>
                )}
              </div>

              {/* Token Metric Pills */}
              <div className="flex flex-wrap items-center gap-1.5" data-testid="token-metrics-row">
                <Badge variant="outline" className="font-mono text-[10px] px-2 py-0.5 bg-muted/40">
                  <span className="text-muted-foreground mr-1">In:</span>
                  <strong className="text-foreground font-semibold">
                    {session?.tokens?.input?.toLocaleString() ?? 0}
                  </strong>
                </Badge>
                <Badge variant="outline" className="font-mono text-[10px] px-2 py-0.5 bg-muted/40">
                  <span className="text-muted-foreground mr-1">Out:</span>
                  <strong className="text-foreground font-semibold">
                    {session?.tokens?.output?.toLocaleString() ?? 0}
                  </strong>
                </Badge>
                {session?.tokens?.reasoning !== undefined && session.tokens.reasoning > 0 && (
                  <Badge variant="outline" className="font-mono text-[10px] px-2 py-0.5 bg-purple-500/10 text-purple-600 dark:text-purple-400 border-purple-500/20">
                    <span className="opacity-80 mr-1">Reasoning:</span>
                    <strong className="font-semibold">{session.tokens.reasoning.toLocaleString()}</strong>
                  </Badge>
                )}
                {session?.tokens?.cache_read !== undefined && session.tokens.cache_read > 0 && (
                  <Badge variant="outline" className="font-mono text-[10px] px-2 py-0.5 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20">
                    <Database className="size-2.5 mr-1" />
                    <span>Cache: {session.tokens.cache_read.toLocaleString()}</span>
                  </Badge>
                )}
                {session?.cost !== undefined && session.cost > 0 && (
                  <Badge variant="outline" className="font-mono text-[10px] px-2 py-0.5 bg-muted/40">
                    <span className="text-muted-foreground mr-1">Cost:</span>
                    <span>${session.cost.toFixed(4)}</span>
                  </Badge>
                )}
              </div>
            </div>

            {compactMessage && (
              <div className="text-xs font-mono text-emerald-600 dark:text-emerald-400 bg-emerald-500/10 border border-emerald-500/20 rounded p-1.5 px-2">
                {compactMessage}
              </div>
            )}
          </div>
        </details>
      </div>

      {/* Main Chat & Conditional Inspector Layout */}
      <div
        className={
          selectedToolKey
            ? 'flex-1 min-h-0 grid grid-cols-1 lg:grid-cols-12 gap-3.5 h-full overflow-hidden relative'
            : 'flex-1 min-h-0 flex flex-col w-full h-full overflow-hidden relative'
        }
      >
        {/* Chat Log & Steer Input */}
        <div
          className={
            selectedToolKey
              ? 'hidden lg:flex lg:col-span-6 flex-col h-full min-h-0 gap-3 overflow-hidden'
              : 'w-full flex flex-col h-full min-h-0 gap-3 overflow-hidden'
          }
        >
          {/* Transcript Body or State banners */}
          <div className="flex-1 min-h-0 flex flex-col rounded-lg border border-border bg-card/60 overflow-hidden relative">
            {session?.children && session.children.length > 0 && (
              <div
                className="flex flex-col gap-1 px-3 py-1.5 border-b border-border text-[11px] font-mono text-muted-foreground shrink-0"
                data-testid="session-children"
              >
                <div className="flex items-center gap-2">
                  <span>Subagents:</span>
                  <span>
                    {session.children.filter((c) => c.status === 'running').length} running
                    {' · '}
                    {session.children.filter((c) => c.status !== 'running').length} finished
                  </span>
                </div>
                <div
                  className="flex flex-wrap items-center gap-1.5 max-h-16 overflow-y-auto"
                  data-testid="session-children-list"
                >
                  {session.children.map((child) => (
                    <span
                      key={child.id}
                      className="max-w-full truncate rounded border border-border px-1.5 py-0.5 text-foreground"
                      title={child.id}
                    >
                      {child.title || child.id}
                      {child.status ? ` · ${child.status}` : ''}
                    </span>
                  ))}
                </div>
              </div>
            )}
            {loading && !session ? (
              <div className="flex-1 flex flex-col items-center justify-center p-8 space-y-2 text-muted-foreground">
                <RefreshCw className="size-6 animate-spin text-primary" />
                <span className="text-xs font-mono">Fetching agent session state...</span>
              </div>
            ) : isSimulated ? (
              <div
                className="flex-1 flex flex-col items-center justify-center p-8 text-center space-y-3"
                data-testid="simulated-session"
              >
                <div className="p-3 rounded-full bg-sky-500/10 text-sky-600 dark:text-sky-400">
                  <Info className="size-6" />
                </div>
                <div className="space-y-1 max-w-sm">
                  <h4 className="text-sm font-semibold text-foreground">Simulated Run</h4>
                  <p className="text-xs text-muted-foreground">
                    This task ran on the development runtime, so no agent session exists. See Terminal Logs for the simulated output. Steering is disabled.
                  </p>
                </div>
              </div>
            ) : isUnavailable ? (
              <div className="flex-1 flex flex-col items-center justify-center p-8 text-center space-y-3">
                <div className="p-3 rounded-full bg-amber-500/10 text-amber-600 dark:text-amber-400">
                  <AlertTriangle className="size-6" />
                </div>
                <div className="space-y-1 max-w-sm">
                  <h4 className="text-sm font-semibold text-foreground">
                    Agent Execution Service Unavailable
                  </h4>
                  <p className="text-xs text-muted-foreground">
                    Task execution is temporarily disabled by the server (503), so this task cannot run or be steered right now. An operator must re-enable the runtime.
                  </p>
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => void refreshSession()}
                  className="text-xs font-mono mt-2"
                  data-testid="retry-session-btn"
                >
                  <RefreshCw className="size-3 mr-1.5" /> Retry Connection
                </Button>
              </div>
            ) : error ? (
              <div className="flex-1 flex flex-col items-center justify-center p-8 text-center space-y-3">
                <div className="p-3 rounded-full bg-destructive/10 text-destructive">
                  <AlertTriangle className="size-6" />
                </div>
                <div className="space-y-1 max-w-sm">
                  <h4 className="text-sm font-semibold text-foreground">Unable to Load Session</h4>
                  <p className="text-xs text-muted-foreground">{error}</p>
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => void refreshSession()}
                  className="text-xs font-mono mt-2"
                  data-testid="retry-session-btn"
                >
                  <RefreshCw className="size-3 mr-1.5" /> Retry
                </Button>
              </div>
            ) : !session?.messages || session.messages.length === 0 ? (
              <div className="flex-1 flex flex-col items-center justify-center p-8 text-center space-y-2 text-muted-foreground">
                <Terminal className="size-6 opacity-40" />
                <p className="text-xs font-mono">No messages recorded in this session transcript yet.</p>
              </div>
            ) : (
              /* Conversation Transcript Scrollable Container */
              <div
                ref={transcriptContainerRef}
                onScroll={handleTranscriptScroll}
                className="flex-1 overflow-y-auto p-4 space-y-4"
                data-testid="session-transcript-container"
              >
                <SessionTranscriptMessages
                  transcript={transcript}
                  expandedTools={expandedTools}
                  selectedToolKey={selectedToolKey}
                  isSteeringActive={isSteeringActive}
                  isSubmittingPrompt={isSubmittingPrompt}
                  toggleToolExpanded={toggleToolExpanded}
                  selectTool={selectTool}
                  submitAnswer={submitAnswer}
                  endRef={transcriptEndRef}
                />
              </div>
            )}

            {/* Floating Scroll-to-Bottom Button */}
            {showScrollBottomBtn && (
              <Button
                type="button"
                variant="secondary"
                size="sm"
                onClick={() => scrollToBottom(true)}
                className="absolute bottom-4 right-4 z-20 rounded-full shadow-lg border border-border bg-background/95 hover:bg-muted text-xs font-medium gap-1.5 transition-all animate-in fade-in slide-in-from-bottom-2"
                data-testid="scroll-to-bottom-btn"
                aria-label="Scroll to bottom"
              >
                <ArrowDown className="size-3.5 text-primary" />
                <span>Scroll to bottom</span>
              </Button>
            )}
          </div>

          {/* Smart Steer Input bar */}
          <form
            onSubmit={handleSendPrompt}
            className="p-3 rounded-lg border border-border bg-card shrink-0 space-y-2.5"
            data-testid="steer-prompt-form"
          >
            {/* Contextual suggestion chips */}
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mr-1">
                Suggestions:
              </span>
              <button
                type="button"
                onClick={() => setSteerPrompt('Fix the failed step and run tests again.')}
                className="px-2 py-0.5 rounded-full text-[11px] font-mono bg-muted/60 hover:bg-muted border border-border/70 text-foreground transition-colors cursor-pointer"
              >
                &quot;Fix failed step&quot;
              </button>
              <button
                type="button"
                onClick={() => setSteerPrompt('Retry subagent execution with the specified guidelines.')}
                className="px-2 py-0.5 rounded-full text-[11px] font-mono bg-muted/60 hover:bg-muted border border-border/70 text-foreground transition-colors cursor-pointer"
              >
                &quot;Retry subagent&quot;
              </button>
              <button
                type="button"
                onClick={() => setSteerPrompt('Resume session and continue with remaining tasks.')}
                className="px-2 py-0.5 rounded-full text-[11px] font-mono bg-muted/60 hover:bg-muted border border-border/70 text-foreground transition-colors cursor-pointer"
              >
                &quot;Resume session&quot;
              </button>
            </div>

            <div className="flex items-center justify-between">
              <label htmlFor="steer-prompt-input" className="text-xs font-semibold text-foreground flex items-center gap-1.5">
                <Sparkles className="size-3.5 text-primary" />
                Agent Steering &amp; Intervention
                {(isSteeringActive || session?.status === 'running') && (
                  <Badge variant="outline" className="font-mono text-[10px] bg-primary/10 text-primary border-primary/20 animate-pulse ml-2">
                    <RefreshCw className="size-2.5 mr-1 animate-spin" />
                    Live Streaming...
                  </Badge>
                )}
              </label>
              <span className="text-[10px] text-muted-foreground font-mono">
                Directly injects prompt into persistent session
              </span>
            </div>

            {steerError && (
              <div className="text-xs text-destructive bg-destructive/10 border border-destructive/20 rounded p-2 font-mono">
                {steerError}
              </div>
            )}

            <div className="flex flex-col sm:flex-row items-stretch sm:items-end gap-2">
              <Textarea
                id="steer-prompt-input"
                value={steerPrompt}
                onChange={(e) => setSteerPrompt(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                    e.preventDefault();
                    handleSendPrompt();
                  }
                }}
                placeholder="Type guidance, bug fix details, or command corrections... (Ctrl+Enter to send)"
                disabled={isSubmittingPrompt || isUnavailable || isSimulated}
                className="w-full sm:flex-1 min-h-[56px] max-h-28 text-xs font-mono resize-none bg-background placeholder:text-muted-foreground"
                data-testid="steer-prompt-textarea"
              />

              <Button
                type="submit"
                disabled={!steerPrompt.trim() || isSubmittingPrompt || isUnavailable || isSimulated}
                size="sm"
                className="h-10 sm:h-14 px-4 text-xs font-medium gap-1.5 bg-primary text-primary-foreground hover:bg-primary/90 shrink-0 self-end sm:self-auto w-full sm:w-auto justify-center"
                data-testid="send-steer-btn"
              >
                {isSubmittingPrompt ? (
                  <>
                    <RefreshCw className="size-3.5 animate-spin" />
                    <span>Sending...</span>
                  </>
                ) : (
                  <>
                    <Send className="size-3.5" />
                    <span>Steer</span>
                  </>
                )}
              </Button>
            </div>
          </form>
        </div>

        {/* Right Pane (Live Tool & Diagnostics Inspector) */}
        {selectedToolKey && (
          <div
            className="w-full lg:col-span-6 flex flex-col h-full min-h-0 rounded-lg border border-border bg-card/95 overflow-hidden shadow-sm"
            data-testid="tool-inspector-panel"
          >
          {selectedToolInfo ? (
            (() => {
              const { tc, msg } = selectedToolInfo;
              const isSubagent = tc.name === 'subagent';
              const inputObj =
                typeof tc.input === 'object' && tc.input !== null
                  ? (tc.input as Record<string, unknown>)
                  : null;
              const subagentAgent = inputObj ? String(inputObj.agent || '') : '';

              const inputStr =
                typeof tc.input === 'string'
                  ? tc.input
                  : tc.input
                  ? JSON.stringify(tc.input, null, 2)
                  : '';

              const outputStr =
                typeof tc.output === 'string'
                  ? tc.output
                  : tc.output
                  ? JSON.stringify(tc.output, null, 2)
                  : '';

              const exitCode = tc.exit_code;
              const isSuccess =
                tc.status !== 'error' && (exitCode === undefined || exitCode === 0);

              // Build detected error snippet or root cause for exit 1
              const detectedError = !isSuccess ? outputStr || 'Process terminated with non-zero exit code.' : null;

              return (
                <div className="flex flex-col h-full min-h-0">
                  {/* Inspector Header */}
                  <div className="p-3 border-b border-border bg-card flex items-center justify-between gap-2 shrink-0">
                    <div className="flex items-center gap-2 min-w-0">
                      {isSubagent ? (
                        <Bot className="size-4 text-purple-400 shrink-0" />
                      ) : (
                        <Terminal className="size-4 text-primary shrink-0" />
                      )}
                      <div className="min-w-0 flex items-center gap-1.5">
                        <span className="font-mono font-semibold text-xs text-foreground truncate">
                          {tc.name}
                        </span>
                        {isSubagent && subagentAgent && (
                          <Badge variant="outline" className="font-mono text-[9px] px-1 py-0 text-purple-400 border-purple-500/30">
                            {subagentAgent}
                          </Badge>
                        )}
                      </div>
                      <div className="shrink-0">
                        {tc.status === 'running' ? (
                          <Badge
                            variant="outline"
                            className="font-mono text-[9px] px-1.5 py-0 text-sky-500 border-sky-500/30 flex items-center gap-1"
                          >
                            <span className="size-1.5 rounded-full bg-sky-500 animate-pulse" />
                            running
                          </Badge>
                        ) : !isSuccess ? (
                          <Badge
                            variant="outline"
                            className="font-mono text-[9px] px-1.5 py-0 bg-rose-500/15 text-rose-600 dark:text-rose-400 border-rose-500/40"
                          >
                            exit {exitCode !== undefined && exitCode !== 0 ? exitCode : 1}
                          </Badge>
                        ) : (
                          <Badge
                            variant="outline"
                            className="font-mono text-[9px] px-1.5 py-0 bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border-emerald-500/30"
                          >
                            exit {exitCode ?? 0}
                          </Badge>
                        )}
                      </div>
                    </div>

                      <div className="flex items-center gap-1">
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="h-7 w-7 text-muted-foreground hover:text-foreground"
                          onClick={() => setSelectedToolKey(null)}
                          title="Close Inspector"
                          aria-label="Close Inspector"
                          data-testid="close-inspector-header-btn"
                        >
                          <X className="size-4" />
                        </Button>
                      </div>
                    </div>

                  {/* Sub-tabs toggle: Output vs Input */}
                  <div className="flex items-center justify-between px-3 py-1.5 border-b border-border/60 bg-muted/30 text-xs">
                    <div className="flex items-center gap-1">
                      <button
                        type="button"
                        onClick={() => setInspectorTab('output')}
                        className={`px-2.5 py-1 rounded text-[11px] font-mono transition-colors cursor-pointer flex items-center gap-1.5 ${
                          inspectorTab === 'output'
                            ? 'bg-card text-foreground font-semibold shadow-sm border border-border/80'
                            : 'text-muted-foreground hover:text-foreground'
                        }`}
                      >
                        <Terminal className="size-3" />
                        <span>Terminal Output (Stdout/Stderr)</span>
                      </button>
                      <button
                        type="button"
                        onClick={() => setInspectorTab('input')}
                        className={`px-2.5 py-1 rounded text-[11px] font-mono transition-colors cursor-pointer flex items-center gap-1.5 ${
                          inspectorTab === 'input'
                            ? 'bg-card text-foreground font-semibold shadow-sm border border-border/80'
                            : 'text-muted-foreground hover:text-foreground'
                        }`}
                      >
                        <FileCode className="size-3" />
                        <span>Prompt Brief / Input</span>
                      </button>
                    </div>

                    {msg.created_at && (
                      <span className="text-[10px] font-mono text-muted-foreground hidden sm:inline">
                        {formatTimestamp(msg.created_at)}
                      </span>
                    )}
                  </div>

                  {/* Inspector Content Body */}
                  <div className="flex-1 min-h-0 overflow-y-auto p-3.5 space-y-3 font-mono text-xs">
                    {/* Detected Error Callout (when exit 1 or failed) */}
                    {!isSuccess && detectedError && (
                      <div className="rounded-lg p-3 bg-rose-500/10 border border-rose-500/30 space-y-1.5">
                        <div className="flex items-center gap-1.5 text-rose-600 dark:text-rose-400 font-semibold text-xs">
                          <AlertCircle className="size-3.5 shrink-0" />
                          <span>Detected Error</span>
                        </div>
                        <p className="text-[11px] text-muted-foreground font-sans">
                          The tool invocation returned non-zero exit status. Review diagnostic logs below or insert a targeted fix into the steer prompt.
                        </p>
                      </div>
                    )}

                    {inspectorTab === 'output' ? (
                      <div className="h-full flex flex-col space-y-2">
                        <div className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground flex items-center justify-between">
                          <div className="flex items-center gap-2">
                            <span>Terminal Logs</span>
                            <span className="font-normal opacity-70">
                              {outputStr ? `${outputStr.split('\n').length} lines` : '0 lines'}
                            </span>
                          </div>
                          {isSubagent && outputStr && (
                            <div className="flex items-center gap-1">
                              <button
                                type="button"
                                onClick={() => setInspectorOutputView('markdown')}
                                className={`px-2 py-0.5 rounded text-[10px] font-mono transition-colors cursor-pointer ${
                                  inspectorOutputView === 'markdown'
                                    ? 'bg-primary/15 text-primary border border-primary/30 font-semibold'
                                    : 'text-muted-foreground hover:text-foreground border border-transparent'
                                }`}
                                data-testid="toggle-output-markdown-btn"
                              >
                                Formatted Report (Markdown)
                              </button>
                              <button
                                type="button"
                                onClick={() => setInspectorOutputView('raw')}
                                className={`px-2 py-0.5 rounded text-[10px] font-mono transition-colors cursor-pointer ${
                                  inspectorOutputView === 'raw'
                                    ? 'bg-primary/15 text-primary border border-primary/30 font-semibold'
                                    : 'text-muted-foreground hover:text-foreground border border-transparent'
                                }`}
                                data-testid="toggle-output-raw-btn"
                              >
                                Raw Logs
                              </button>
                            </div>
                          )}
                        </div>
                        {tc.status === 'running' ? (
                          <div className="flex items-center gap-2 p-4 rounded bg-muted/20 border border-border/50 text-xs text-primary">
                            <RefreshCw className="size-4 animate-spin text-primary" />
                            <span>Execution running in child environment...</span>
                          </div>
                        ) : outputStr ? (
                          isSubagent && inspectorOutputView === 'markdown' ? (
                            <div className="flex-1 min-h-[260px] p-3.5 rounded-lg border border-border/60 bg-muted/20 overflow-x-auto text-xs font-sans">
                              <Markdown content={outputStr} />
                            </div>
                          ) : (
                            <pre
                              className={`flex-1 min-h-[260px] p-3.5 rounded-lg overflow-x-auto whitespace-pre-wrap leading-relaxed text-xs font-mono border ${
                                !isSuccess
                                  ? 'bg-rose-950/20 text-rose-200 border-rose-500/30'
                                  : 'bg-black/40 text-foreground border-border/50'
                              }`}
                            >
                              {outputStr}
                            </pre>
                          )
                        ) : (
                          <div className="p-4 text-center text-muted-foreground italic text-xs">
                            No terminal output recorded for this tool run.
                          </div>
                        )}
                      </div>
                    ) : (
                      <div className="h-full flex flex-col space-y-2">
                        {/* If subagent description exists, show card badge */}
                        {Boolean(inputObj?.description) && (
                          <div className="p-2.5 rounded-lg bg-purple-500/10 border border-purple-500/20 text-xs font-sans space-y-1">
                            <div className="font-semibold text-purple-600 dark:text-purple-300 text-[11px] uppercase tracking-wider">
                              Subagent Objective:
                            </div>
                            <div className="text-foreground leading-relaxed">
                              {String(inputObj?.description)}
                            </div>
                          </div>
                        )}

                        <div className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground flex items-center justify-between">
                          <div className="flex items-center gap-2">
                            <span>Command / Prompt Input</span>
                            <span className="font-normal opacity-70">
                              {inputStr ? `${inputStr.split('\n').length} lines` : '0 lines'}
                            </span>
                          </div>
                          {(isSubagent || inputObj?.prompt || typeof tc.input === 'string') && inputStr && (
                            <div className="flex items-center gap-1">
                              <button
                                type="button"
                                onClick={() => setInspectorInputView('markdown')}
                                className={`px-2 py-0.5 rounded text-[10px] font-mono transition-colors cursor-pointer ${
                                  inspectorInputView === 'markdown'
                                    ? 'bg-primary/15 text-primary border border-primary/30 font-semibold'
                                    : 'text-muted-foreground hover:text-foreground border border-transparent'
                                }`}
                                data-testid="toggle-input-markdown-btn"
                              >
                                Formatted Brief (Markdown)
                              </button>
                              <button
                                type="button"
                                onClick={() => setInspectorInputView('raw')}
                                className={`px-2 py-0.5 rounded text-[10px] font-mono transition-colors cursor-pointer ${
                                  inspectorInputView === 'raw'
                                    ? 'bg-primary/15 text-primary border border-primary/30 font-semibold'
                                    : 'text-muted-foreground hover:text-foreground border border-transparent'
                                }`}
                                data-testid="toggle-input-raw-btn"
                              >
                                Raw Input (JSON)
                              </button>
                            </div>
                          )}
                        </div>

                        {inputStr ? (
                          (isSubagent || inputObj?.prompt || typeof tc.input === 'string') && inspectorInputView === 'markdown' ? (
                            <div className="flex-1 min-h-[260px] p-3.5 rounded-lg bg-muted/20 border border-border/60 overflow-x-auto text-xs font-sans leading-relaxed">
                              <Markdown
                                content={
                                  inputObj?.prompt
                                    ? String(inputObj.prompt)
                                    : typeof tc.input === 'string'
                                    ? tc.input
                                    : inputStr
                                }
                              />
                            </div>
                          ) : (
                            <pre className="flex-1 min-h-[260px] p-3.5 rounded-lg bg-black/40 text-foreground border border-border/50 overflow-x-auto whitespace-pre-wrap leading-relaxed text-xs font-mono">
                              {inputStr}
                            </pre>
                          )
                        ) : (
                          <div className="p-4 text-center text-muted-foreground italic text-xs">
                            No input arguments provided.
                          </div>
                        )}
                      </div>
                    )}
                  </div>

                  {/* Inspector Footer Actions */}
                  <div className="p-2.5 border-t border-border bg-card/90 flex flex-wrap items-center justify-between gap-2 shrink-0">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => setSelectedToolKey(null)}
                      className="h-7 text-xs font-mono gap-1 text-muted-foreground hover:text-foreground"
                      data-testid="close-inspector-footer-btn"
                    >
                      <X className="size-3" />
                      <span>Close / Unselect</span>
                    </Button>

                    <Button
                      type="button"
                      variant="default"
                      size="sm"
                      onClick={() => {
                        const snippet = (!isSuccess && detectedError ? detectedError : outputStr || inputStr)
                          .trim()
                          .slice(0, 500);
                        const fixMessage = `Fix the error in tool "${tc.name}":\n${snippet}\nPlease resolve and re-run.`;
                        setSteerPrompt(fixMessage);
                      }}
                      className="h-7 text-xs font-mono gap-1.5 bg-primary text-primary-foreground hover:bg-primary/90"
                      data-testid="insert-fix-btn"
                    >
                      <CornerDownLeft className="size-3" />
                      <span>Insert Fix into Steer Input ↵</span>
                    </Button>
                  </div>
                </div>
              );
            })()
          ) : null}
        </div>
      )}
      </div>
    </div>
  );
};
