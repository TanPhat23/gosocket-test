import { useState } from 'react';
import { Check, ChevronDown, Copy, Database, RefreshCw, Square, Zap } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import type { TaskSessionResponse } from '../../schemas/taskSchemas';
import type { useSessionActions } from '../../hooks/useSessionActions';
import { SessionStatusBadge } from './SessionStatusBadge';

interface SessionInfoBarProps {
  session: TaskSessionResponse | null;
  sessionId: string;
  // False when the session cannot be read (unavailable or simulated), so its status would be misleading.
  showStatus: boolean;
  loading: boolean;
  controlsDisabled: boolean;
  actions: ReturnType<typeof useSessionActions>;
  onRefresh: () => void;
}

const ghostButton = 'h-7 gap-1.5 px-1.5 sm:px-2 text-xs text-muted-foreground hover:text-foreground';

function TokenMetrics({ session }: { session: TaskSessionResponse | null }) {
  const pill = 'font-mono text-[10px] px-2 py-0.5';
  const tokens = session?.tokens;
  return (
    <div className="flex flex-wrap items-center gap-1.5" data-testid="token-metrics-row">
      <Badge variant="outline" className={`${pill} bg-muted/40`}>
        <span className="text-muted-foreground mr-1">In:</span>
        <strong className="text-foreground font-semibold">{tokens?.input?.toLocaleString() ?? 0}</strong>
      </Badge>
      <Badge variant="outline" className={`${pill} bg-muted/40`}>
        <span className="text-muted-foreground mr-1">Out:</span>
        <strong className="text-foreground font-semibold">{tokens?.output?.toLocaleString() ?? 0}</strong>
      </Badge>
      {tokens?.reasoning !== undefined && tokens.reasoning > 0 && (
        <Badge variant="outline" className={`${pill} bg-purple-500/10 text-purple-600 dark:text-purple-400 border-purple-500/20`}>
          <span className="opacity-80 mr-1">Reasoning:</span>
          <strong className="font-semibold">{tokens.reasoning.toLocaleString()}</strong>
        </Badge>
      )}
      {tokens?.cache_read !== undefined && tokens.cache_read > 0 && (
        <Badge variant="outline" className={`${pill} bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20`}>
          <Database className="size-2.5 mr-1" />
          <span>Cache: {tokens.cache_read.toLocaleString()}</span>
        </Badge>
      )}
      {session?.cost !== undefined && session.cost > 0 && (
        <Badge variant="outline" className={`${pill} bg-muted/40`}>
          <span className="text-muted-foreground mr-1">Cost:</span>
          <span>${session.cost.toFixed(4)}</span>
        </Badge>
      )}
    </div>
  );
}

function CopySessionIdButton({ sessionId }: { sessionId: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(sessionId);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // ignore clipboard error
    }
  };
  return (
    <Button type="button" variant="outline" size="sm" onClick={copy} className={`${ghostButton} ml-2`} data-testid="copy-session-id-btn">
      {copied ? (
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
  );
}

// The session's status, id and quick actions on one line; its model, token use and cost fold out below.
export function SessionInfoBar({ session, sessionId, showStatus, loading, controlsDisabled, actions, onRefresh }: SessionInfoBarProps) {
  return (
    <div className="shrink-0 rounded-xl border border-border bg-card text-xs">
      <details className="group">
        <summary className="flex cursor-pointer select-none items-center justify-between gap-2 rounded-xl px-3 py-2 transition-colors hover:bg-muted/30">
          <div className="flex items-center gap-2 min-w-0">
            {showStatus && <SessionStatusBadge status={session?.status} />}
            <span className="hidden font-semibold text-foreground sm:inline">Session Info</span>
            <Badge
              variant="secondary"
              className="max-w-[6.5rem] select-all truncate bg-muted/60 font-mono text-[10px] text-muted-foreground sm:max-w-xs"
              title={sessionId}
              data-testid="session-id-display"
              onClick={(e) => e.stopPropagation()}
            >
              {sessionId}
            </Badge>
          </div>

          <div className="flex shrink-0 items-center gap-1">
            <div className="flex items-center gap-0.5 sm:gap-1.5" onClick={(e) => e.stopPropagation()}>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={loading || actions.isCompacting || controlsDisabled}
                onClick={actions.compact}
                className={ghostButton}
                title="Compact session context"
                data-testid="compact-session-btn"
              >
                <Zap className={`size-3 text-amber-500 ${actions.isCompacting ? 'animate-spin' : ''}`} />
                <span className="hidden sm:inline">{actions.isCompacting ? 'Compacting...' : 'Compact'}</span>
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={loading || actions.isInterrupting || controlsDisabled}
                onClick={actions.interrupt}
                className={ghostButton}
                title="Interrupt the running turn (the task is not cancelled)"
                data-testid="interrupt-session-btn"
              >
                <Square className="size-3 text-red-500" />
                <span className="hidden sm:inline">{actions.isInterrupting ? 'Interrupting...' : 'Interrupt'}</span>
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={loading}
                onClick={onRefresh}
                className={ghostButton}
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

        <div className="p-3 pt-0 border-t border-border/60 space-y-2 mt-1">
          <div className="flex flex-wrap items-center justify-between gap-2 pt-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-muted-foreground text-[11px] uppercase tracking-wider font-semibold">Model:</span>
              <span className="font-mono text-xs">{session?.model || 'default'}</span>
              {sessionId !== 'Unavailable' && <CopySessionIdButton sessionId={sessionId} />}
            </div>
            <TokenMetrics session={session} />
          </div>

          {actions.message && (
            <div className="text-xs font-mono text-emerald-600 dark:text-emerald-400 bg-emerald-500/10 border border-emerald-500/20 rounded p-1.5 px-2">
              {actions.message}
            </div>
          )}
        </div>
      </details>
    </div>
  );
}
