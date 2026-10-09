import React, { useState, useEffect, useRef } from 'react';
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
  Cpu,
} from 'lucide-react';

interface AgentSessionViewProps {
  taskId: string;
  defaultSessionId?: string | null;
}

export interface ParsedMessageData {
  text: string;
  thoughts?: string;
  toolCalls: SessionToolCall[];
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

  return {
    text: textParts.join('\n\n'),
    thoughts: thoughtParts.join('\n\n') || undefined,
    toolCalls,
  };
}

export const AgentSessionView: React.FC<AgentSessionViewProps> = ({ taskId, defaultSessionId }) => {
  const [session, setSession] = useState<TaskSessionResponse | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [isUnavailable, setIsUnavailable] = useState<boolean>(false);
  const [copiedSessionId, setCopiedSessionId] = useState<boolean>(false);

  // Steer prompt state
  const [steerPrompt, setSteerPrompt] = useState<string>('');
  const [isSubmittingPrompt, setIsSubmittingPrompt] = useState<boolean>(false);
  const [isSteeringActive, setIsSteeringActive] = useState<boolean>(false);
  const [steerError, setSteerError] = useState<string | null>(null);

  // Compact state
  const [isCompacting, setIsCompacting] = useState<boolean>(false);
  const [compactMessage, setCompactMessage] = useState<string | null>(null);

  // Expanded tool calls tracker: msgIndex-toolIndex
  const [expandedTools, setExpandedTools] = useState<Record<string, boolean>>({});

  const transcriptEndRef = useRef<HTMLDivElement>(null);

  const fetchSession = async (showLoadingSpinner = true) => {
    if (showLoadingSpinner) {
      setLoading(true);
    }
    setError(null);
    setIsUnavailable(false);
    try {
      const data = await taskApi.getTaskSession(taskId);
      setSession(data);
    } catch (err: unknown) {
      const apiErr = err as { status?: number; message?: string };
      if (apiErr?.status === 503) {
        setIsUnavailable(true);
        setError('Agent execution service is currently unavailable (OpenCode daemon offline).');
      } else if (apiErr?.status === 404) {
        setError('No active session initialized for this task yet.');
      } else {
        setError(apiErr?.message || 'Failed to load task session.');
      }
    } finally {
      if (showLoadingSpinner) {
        setLoading(false);
      }
    }
  };

  useEffect(() => {
    fetchSession(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [taskId]);

  // Adaptive auto-refresh when session is in active running state or user is steering
  useEffect(() => {
    const isRunning = session?.status === 'running' || isSteeringActive;
    if (!isRunning) return;

    let pollCount = 0;
    const maxPolls = 60; // 60 * 800ms = 48s max
    const interval = setInterval(async () => {
      pollCount++;
      await fetchSession(false);
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
  }, [session?.status, isSteeringActive, taskId]);

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
            fetchSession(false);
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

  const handleSendPrompt = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    const trimmed = steerPrompt.trim();
    if (!trimmed || isSubmittingPrompt) return;

    const userPromptText = trimmed;
    setIsSubmittingPrompt(true);
    setSteerError(null);
    setSteerPrompt('');
    setIsSteeringActive(true);

    // Optimistically inject user prompt so it displays immediately
    const optimisticMsg: SessionMessage = {
      id: `opt-user-${Date.now()}`,
      type: 'user',
      role: 'user',
      text: userPromptText,
      created_at: new Date().toISOString(),
    };
    setSession((prev) => {
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
      await fetchSession(false);
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

  const handleCompactSession = async () => {
    setIsCompacting(true);
    setCompactMessage(null);
    try {
      await taskApi.compactTaskSession(taskId);
      setCompactMessage('Session context successfully compacted.');
      await fetchSession(false);
      setTimeout(() => setCompactMessage(null), 4000);
    } catch (err: unknown) {
      const apiErr = err as { message?: string };
      setCompactMessage(`Compaction failed: ${apiErr?.message || 'Unknown error'}`);
    } finally {
      setIsCompacting(false);
    }
  };

  const toggleToolExpanded = (key: string) => {
    setExpandedTools((prev) => ({
      ...prev,
      [key]: !prev[key],
    }));
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

  const formatTimestamp = (ts?: number | string | null) => {
    if (!ts) return null;
    try {
      const date = typeof ts === 'number' ? new Date(ts) : new Date(ts);
      return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    } catch {
      return null;
    }
  };

  return (
    <div className="flex flex-col h-full overflow-hidden bg-background text-foreground space-y-3">
      {/* Session Header */}
      <div className="p-3 rounded-lg border border-border bg-card shrink-0 space-y-2.5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          {/* Session ID & Copy */}
          <div className="flex items-center gap-2 min-w-0">
            <span className="text-xs font-medium text-muted-foreground flex items-center gap-1.5 shrink-0">
              <Bot className="size-3.5 text-primary" /> Session ID:
            </span>
            <Badge
              variant="secondary"
              className="font-mono text-xs truncate max-w-[220px] sm:max-w-xs select-all bg-muted/60"
              title={currentSessionId}
              data-testid="session-id-display"
            >
              {currentSessionId}
            </Badge>
            {currentSessionId !== 'Unavailable' && (
              <Button
                type="button"
                variant="ghost"
                size="icon"
                onClick={() => handleCopySessionId(currentSessionId)}
                className="h-6 w-6 shrink-0 text-muted-foreground hover:text-foreground"
                title={copiedSessionId ? 'Copied' : 'Copy Session ID'}
                aria-label={copiedSessionId ? 'Copied Session ID' : 'Copy Session ID'}
                data-testid="copy-session-id-btn"
              >
                {copiedSessionId ? (
                  <Check className="size-3 text-emerald-500" />
                ) : (
                  <Copy className="size-3" />
                )}
              </Button>
            )}
          </div>

          {/* Action buttons: Compact & Refresh */}
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={loading || isCompacting || isUnavailable}
              onClick={handleCompactSession}
              className="h-7 text-xs font-mono gap-1.5 hover:bg-muted"
              title="Compact session context to free token window"
              data-testid="compact-session-btn"
            >
              <Zap className={`size-3 ${isCompacting ? 'animate-spin text-amber-500' : 'text-amber-500'}`} />
              <span>{isCompacting ? 'Compacting...' : 'Compact Session'}</span>
            </Button>

            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={loading}
              onClick={() => fetchSession(false)}
              className="h-7 text-xs font-mono gap-1.5 hover:bg-muted"
              title="Refresh session transcript and metrics"
              data-testid="refresh-session-btn"
            >
              <RefreshCw className={`size-3 ${loading ? 'animate-spin' : ''}`} />
              <span>Refresh</span>
            </Button>
          </div>
        </div>

        {/* Status, Model, and Token Badges Row */}
        <div className="flex flex-wrap items-center justify-between gap-2 pt-2 border-t border-border/50 text-xs">
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground text-[11px] uppercase tracking-wider font-semibold">
              Status:
            </span>
            {renderStatusBadge(session?.status)}
            {session?.model && (
              <Badge variant="outline" className="font-mono text-[10px] gap-1 text-muted-foreground">
                <Cpu className="size-2.5" />
                {session.model}
              </Badge>
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

        {/* Compact Success Message */}
        {compactMessage && (
          <div className="text-xs font-mono text-emerald-600 dark:text-emerald-400 bg-emerald-500/10 border border-emerald-500/20 rounded p-1.5 px-2">
            {compactMessage}
          </div>
        )}
      </div>

      {/* Main Transcript Body or State banners */}
      <div className="flex-1 min-h-0 flex flex-col rounded-lg border border-border bg-card/60 overflow-hidden">
        {loading && !session ? (
          <div className="flex-1 flex flex-col items-center justify-center p-8 space-y-2 text-muted-foreground">
            <RefreshCw className="size-6 animate-spin text-primary" />
            <span className="text-xs font-mono">Fetching agent session state...</span>
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
                The OpenCode daemon server is currently offline or unreachable (503). Ensure the background runner is initialized.
              </p>
            </div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => fetchSession(true)}
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
              onClick={() => fetchSession(true)}
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
            className="flex-1 overflow-y-auto p-4 space-y-4"
            data-testid="session-transcript-container"
          >
            {[...(session.messages || [])]
              .sort((a, b) => {
                const tA = a.created_at ? new Date(a.created_at).getTime() : 0;
                const tB = b.created_at ? new Date(b.created_at).getTime() : 0;
                return tA - tB;
              })
              .map((msg: SessionMessage, msgIdx: number) => {
              const isUser = msg.role === 'user' || msg.type === 'user';
              const parsed = parseSessionMessageContent(msg);
              const { text: textContent, thoughts, toolCalls } = parsed;
              const timeString = formatTimestamp(msg.created_at);

              // Skip rendering empty assistant bubbles with no text, thoughts, or tools
              if (!isUser && !textContent && !thoughts && toolCalls.length === 0) {
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

                return (
                  <div
                    key={msg.id || `user-${msgIdx}`}
                    className="flex justify-end"
                    data-testid="user-message"
                  >
                    <div className="max-w-[85%] rounded-lg p-3 bg-primary text-primary-foreground text-xs space-y-1.5 shadow-sm">
                      <div className="flex items-center justify-between gap-3 text-[10px] opacity-80 border-b border-primary-foreground/20 pb-1">
                        <span className="font-semibold flex items-center gap-1">
                          <User className="size-3" /> User Prompt
                        </span>
                        {timeString && <span className="font-mono">{timeString}</span>}
                      </div>
                      <p className="whitespace-pre-wrap leading-relaxed font-sans">{cleanUserText}</p>
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
                      <details className="text-[11px] text-muted-foreground bg-muted/50 rounded p-2 border border-border/40 group">
                        <summary className="cursor-pointer font-medium flex items-center gap-1.5 select-none text-muted-foreground hover:text-foreground">
                          <Sparkles className="size-3 text-purple-500" />
                          <span>Agent Reasoning</span>
                        </summary>
                        <div className="mt-2 whitespace-pre-wrap font-mono text-[10.5px] leading-relaxed opacity-90 pl-3 border-l-2 border-purple-500/30">
                          {thoughts}
                        </div>
                      </details>
                    )}

                    {/* Text reply */}
                    {textContent && (
                      <div className="whitespace-pre-wrap leading-relaxed text-foreground font-sans text-xs">
                        {textContent}
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
                            const expandKey = `${msgIdx}-${tcIdx}`;
                            const isExpanded = !!expandedTools[expandKey];

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
                                className="rounded border border-border/80 bg-background/80 overflow-hidden font-mono text-[11px]"
                                data-testid="tool-call-card"
                              >
                                <button
                                  type="button"
                                  onClick={() => toggleToolExpanded(expandKey)}
                                  className="w-full flex items-center justify-between p-2 bg-muted/40 hover:bg-muted/60 transition-colors text-left cursor-pointer"
                                  data-testid={`toggle-tool-${tc.name}`}
                                >
                                  <div className="flex items-center gap-2 min-w-0">
                                    {isExpanded ? (
                                      <ChevronDown className="size-3 text-muted-foreground shrink-0" />
                                    ) : (
                                      <ChevronRight className="size-3 text-muted-foreground shrink-0" />
                                    )}
                                    <Badge
                                      variant="outline"
                                      className={`font-mono text-[10px] px-1.5 py-0 ${
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
                                        className="font-mono text-[9px] px-1 py-0 bg-primary/10 text-primary border-primary/20"
                                      >
                                        {subagentAgent}
                                      </Badge>
                                    )}
                                    <span className="truncate text-muted-foreground max-w-xs sm:max-w-md">
                                      {isSubagent && subagentDesc
                                        ? subagentDesc
                                        : inputStr.replace(/\n/g, ' ')}
                                    </span>
                                  </div>

                                  <div className="flex items-center gap-2 shrink-0">
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
                                        className="font-mono text-[9px] px-1 py-0 text-rose-500 border-rose-500/30"
                                      >
                                        {exitCode !== undefined && exitCode !== 0
                                          ? `exit ${exitCode}`
                                          : 'failed'}
                                      </Badge>
                                    ) : exitCode !== undefined ? (
                                      <Badge
                                        variant="outline"
                                        className="font-mono text-[9px] px-1 py-0 text-emerald-600 dark:text-emerald-400 border-emerald-500/30"
                                      >
                                        exit {exitCode}
                                      </Badge>
                                    ) : null}
                                  </div>
                                </button>

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
                                        <pre className="p-2 rounded bg-muted/50 text-[10px] overflow-x-auto whitespace-pre-wrap text-foreground max-h-48 font-mono">
                                          {isSubagent && inputObj?.prompt
                                            ? String(inputObj.prompt)
                                            : inputStr}
                                        </pre>
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
            <div ref={transcriptEndRef} />
          </div>
        )}
      </div>

      {/* Interactive Steering Form */}
      <form
        onSubmit={handleSendPrompt}
        className="p-3 rounded-lg border border-border bg-card shrink-0 space-y-2"
        data-testid="steer-prompt-form"
      >
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

        <div className="flex items-end gap-2">
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
            disabled={isSubmittingPrompt || isUnavailable}
            className="flex-1 min-h-[56px] max-h-28 text-xs font-mono resize-none bg-background placeholder:text-muted-foreground"
            data-testid="steer-prompt-textarea"
          />

          <Button
            type="submit"
            disabled={!steerPrompt.trim() || isSubmittingPrompt || isUnavailable}
            size="sm"
            className="h-14 px-4 text-xs font-medium gap-1.5 bg-primary text-primary-foreground hover:bg-primary/90 shrink-0"
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
  );
};
