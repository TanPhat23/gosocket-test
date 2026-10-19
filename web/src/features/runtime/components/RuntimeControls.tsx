import React from 'react';
import { useQuery } from '@tanstack/react-query';
import { Loader2, Play, RotateCw, Square } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { runtimeQueryOptions } from '../queries';
import { useRuntimeActions } from '../hooks/useRuntimeActions';

interface RuntimeControlsProps {
  projectId: string;
  // Stop and restart need a project admin; the server enforces it, the buttons just do not offer what would fail.
  canManage: boolean;
}

// Start, stop and restart for the project's container. A stop or restart refused because tasks are running is
// offered again as a confirmed, forced one.
export const RuntimeControls: React.FC<RuntimeControlsProps> = ({ projectId, canManage }) => {
  const { data: info } = useQuery(runtimeQueryOptions(projectId));
  const actions = useRuntimeActions(projectId);
  const state = info?.state;
  const up = state === 'running' || state === 'starting';
  const working = actions.pending !== null;

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          onClick={() => actions.run('start')}
          disabled={working || !state || up}
          data-testid="runtime-start-btn"
        >
          {actions.pending === 'start' ? <Loader2 className="animate-spin" /> : <Play />}
          Start
        </Button>
        <Button
          size="sm"
          variant="outline"
          onClick={() => actions.run('restart')}
          disabled={working || !canManage || state !== 'running'}
          data-testid="runtime-restart-btn"
        >
          {actions.pending === 'restart' ? <Loader2 className="animate-spin" /> : <RotateCw />}
          Restart
        </Button>
        <Button
          size="sm"
          variant="outline"
          onClick={() => actions.run('stop')}
          disabled={working || !canManage || state !== 'running'}
          data-testid="runtime-stop-btn"
        >
          {actions.pending === 'stop' ? <Loader2 className="animate-spin" /> : <Square />}
          Stop
        </Button>
        {!canManage && <span className="self-center text-xs text-muted-foreground">Stop and restart need an admin.</span>}
      </div>

      {actions.busy && (
        <div
          role="alert"
          className="rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-sm space-y-2"
          data-testid="runtime-busy-warning"
        >
          <p>{actions.busy.message}</p>
          <p className="text-xs text-muted-foreground">
            {actions.busy.action === 'stop' ? 'Stopping' : 'Restarting'} now interrupts the tasks that are running.
          </p>
          <div className="flex gap-2">
            <Button size="xs" variant="destructive" onClick={actions.force} data-testid="runtime-force-btn">
              {actions.busy.action === 'stop' ? 'Stop anyway' : 'Restart anyway'}
            </Button>
            <Button size="xs" variant="ghost" onClick={actions.dismissBusy}>
              Cancel
            </Button>
          </div>
        </div>
      )}

      {actions.error && (
        <p className="text-sm text-destructive" role="alert" data-testid="runtime-action-error">
          {actions.error}
        </p>
      )}
    </div>
  );
};
