import { AlertTriangle, Info, RefreshCw, Terminal } from 'lucide-react';
import type { ReactNode } from 'react';
import { Button } from '@/components/ui/button';

interface SessionStateBannerProps {
  state: 'loading' | 'simulated' | 'unavailable' | 'error' | 'empty';
  error?: string | null;
  onRetry: () => void;
}

function Centered({ children, testId, gap = 'space-y-3' }: { children: ReactNode; testId?: string; gap?: string }) {
  return (
    <div className={`flex-1 flex flex-col items-center justify-center p-8 text-center ${gap}`} data-testid={testId}>
      {children}
    </div>
  );
}

function Notice({ icon, tone, title, children }: { icon: ReactNode; tone: string; title: string; children: ReactNode }) {
  return (
    <>
      <div className={`p-3 rounded-full ${tone}`}>{icon}</div>
      <div className="space-y-1 max-w-sm">
        <h4 className="text-sm font-semibold text-foreground">{title}</h4>
        <p className="text-xs text-muted-foreground">{children}</p>
      </div>
    </>
  );
}

function RetryButton({ label, onRetry }: { label: string; onRetry: () => void }) {
  return (
    <Button type="button" variant="outline" size="sm" onClick={onRetry} className="text-xs font-mono mt-2" data-testid="retry-session-btn">
      <RefreshCw className="size-3 mr-1.5" /> {label}
    </Button>
  );
}

// What the transcript pane shows instead of messages when there is nothing to read yet.
export function SessionStateBanner({ state, error, onRetry }: SessionStateBannerProps) {
  switch (state) {
    case 'loading':
      return (
        <Centered gap="space-y-2">
          <RefreshCw className="size-6 animate-spin text-primary" />
          <span className="text-xs font-mono text-muted-foreground">Fetching agent session state...</span>
        </Centered>
      );
    case 'simulated':
      return (
        <Centered testId="simulated-session">
          <Notice icon={<Info className="size-6" />} tone="bg-sky-500/10 text-sky-600 dark:text-sky-400" title="Simulated Run">
            This task ran on the development runtime, so no agent session exists. See Terminal Logs for the simulated output. Steering is disabled.
          </Notice>
        </Centered>
      );
    case 'unavailable':
      return (
        <Centered>
          <Notice
            icon={<AlertTriangle className="size-6" />}
            tone="bg-amber-500/10 text-amber-600 dark:text-amber-400"
            title="Agent Execution Service Unavailable"
          >
            Task execution is temporarily disabled by the server (503), so this task cannot run or be steered right now. An operator must re-enable the runtime.
          </Notice>
          <RetryButton label="Retry Connection" onRetry={onRetry} />
        </Centered>
      );
    case 'error':
      return (
        <Centered>
          <Notice icon={<AlertTriangle className="size-6" />} tone="bg-destructive/10 text-destructive" title="Unable to Load Session">
            {error}
          </Notice>
          <RetryButton label="Retry" onRetry={onRetry} />
        </Centered>
      );
    case 'empty':
      return (
        <Centered gap="space-y-2">
          <Terminal className="size-6 opacity-40 text-muted-foreground" />
          <p className="text-xs font-mono text-muted-foreground">No messages recorded in this session transcript yet.</p>
        </Centered>
      );
  }
}
