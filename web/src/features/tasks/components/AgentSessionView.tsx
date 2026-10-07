import React, { useState, useEffect, useRef } from 'react';
import { taskApi } from '../api/taskApi';
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

export const AgentSessionView: React.FC<AgentSessionViewProps> = ({ taskId, defaultSessionId }) => {
  const [session, setSession] = useState<TaskSessionResponse | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [isUnavailable, setIsUnavailable] = useState<boolean>(false);
  const [copiedSessionId, setCopiedSessionId] = useState<boolean>(false);

  // Steer prompt state
  const [steerPrompt, setSteerPrompt] = useState<string>('');
  const [isSubmittingPrompt, setIsSubmittingPrompt] = useState<boolean>(false);
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

    setIsSubmittingPrompt(true);
    setSteerError(null);
    try {
      await taskApi.promptTaskSession(taskId, trimmed);
      setSteerPrompt('');
      // Refresh transcript
      await fetchSession(false);
      // Scroll down
      setTimeout(() => {
        transcriptEndRef.current?.scrollIntoView({ behavior: 'smooth' });
      }, 100);
    } catch (err: unknown) {
      const apiErr = err as { message?: string };
      setSteerError(apiErr?.message || 'Failed to deliver steer prompt.');
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
            {session.messages.map((msg: SessionMessage, msgIdx: number) => {
              const isUser = msg.role === 'user' || msg.type === 'user';
              const textContent =
                msg.text ||
                (typeof msg.content === 'string'
                  ? msg.content
                  : typeof msg.content === 'object' && msg.content !== null
                  ? JSON.stringify(msg.content, null, 2)
                  : '');

              const toolCalls = msg.tool_calls || [];
              const timeString = formatTimestamp(msg.created_at);

              if (isUser) {
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
                      <p className="whitespace-pre-wrap leading-relaxed font-sans">{textContent}</p>
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
                            const isSuccess = exitCode === undefined || exitCode === 0;

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
                                    <Badge variant="outline" className="font-mono text-[10px] px-1.5 py-0 bg-primary/10 text-primary border-primary/20">
                                      {tc.name}
                                    </Badge>
                                    <span className="truncate text-muted-foreground max-w-xs sm:max-w-md">
                                      {inputStr.replace(/\n/g, ' ')}
                                    </span>
                                  </div>

                                  <div className="flex items-center gap-2 shrink-0">
                                    {exitCode !== undefined && (
                                      <Badge
                                        variant="outline"
                                        className={`font-mono text-[9px] px-1 py-0 ${
                                          isSuccess
                                            ? 'text-emerald-600 dark:text-emerald-400 border-emerald-500/30'
                                            : 'text-rose-500 border-rose-500/30'
                                        }`}
                                      >
                                        exit {exitCode}
                                      </Badge>
                                    )}
                                  </div>
                                </button>

                                {isExpanded && (
                                  <div className="p-2.5 border-t border-border/60 space-y-2 bg-background">
                                    {inputStr && (
                                      <div className="space-y-1">
                                        <div className="text-[10px] text-muted-foreground uppercase font-semibold">
                                          Input
                                        </div>
                                        <pre className="p-2 rounded bg-muted/50 text-[10px] overflow-x-auto whitespace-pre-wrap text-foreground">
                                          {inputStr}
                                        </pre>
                                      </div>
                                    )}
                                    {outputStr && (
                                      <div className="space-y-1">
                                        <div className="text-[10px] text-muted-foreground uppercase font-semibold">
                                          Output
                                        </div>
                                        <pre className="p-2 rounded bg-muted/50 text-[10px] overflow-x-auto whitespace-pre-wrap max-h-48 text-foreground">
                                          {outputStr}
                                        </pre>
                                      </div>
                                    )}
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
