import React from 'react';
import { useQuery } from '@tanstack/react-query';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { runtimeQueryOptions } from '../queries';
import type { RuntimeState } from '../schemas/runtimeSchemas';
import { formatBytes, formatUptime } from '../utils/format';

const STATE_LABEL: Record<RuntimeState, string> = {
  running: 'Running',
  starting: 'Starting',
  stopped: 'Stopped',
  absent: 'Not created',
};

const STATE_DOT: Record<RuntimeState, string> = {
  running: 'bg-emerald-500',
  starting: 'bg-amber-500 animate-pulse',
  stopped: 'bg-muted-foreground/60',
  absent: 'bg-muted-foreground/30',
};

const Stat: React.FC<{ label: string; value: React.ReactNode; testId: string }> = ({ label, value, testId }) => (
  <div className="space-y-1">
    <dt className="text-[11px] uppercase tracking-wide text-muted-foreground">{label}</dt>
    <dd className="text-sm font-medium font-mono truncate" data-testid={testId}>
      {value}
    </dd>
  </div>
);

// The container's state, and when it is running, its uptime and resource use.
export const RuntimeStatusPanel: React.FC<{ projectId: string }> = ({ projectId }) => {
  const { data: info, error, isPending } = useQuery(runtimeQueryOptions(projectId));

  if (isPending) {
    return <div className="h-32 rounded-xl bg-muted/40 animate-pulse" data-testid="runtime-status-loading" />;
  }
  if (error || !info) {
    return (
      <Card size="sm">
        <CardContent className="text-sm text-destructive" data-testid="runtime-status-error">
          {error instanceof Error ? error.message : 'Could not read the container state'}
        </CardContent>
      </Card>
    );
  }

  const running = info.state === 'running';
  const memory =
    running && info.memory_bytes !== undefined
      ? info.memory_limit_bytes
        ? `${formatBytes(info.memory_bytes)} / ${formatBytes(info.memory_limit_bytes)}`
        : formatBytes(info.memory_bytes)
      : '—';
  const memoryShare =
    running && info.memory_bytes !== undefined && info.memory_limit_bytes
      ? Math.min(100, (info.memory_bytes / info.memory_limit_bytes) * 100)
      : null;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <span className={`size-2 rounded-full ${STATE_DOT[info.state]}`} aria-hidden="true" />
          <Badge variant={running ? 'default' : 'secondary'} data-testid="runtime-state">
            {STATE_LABEL[info.state]}
          </Badge>
        </CardTitle>
      </CardHeader>
      <CardContent>
        <dl className="grid grid-cols-2 md:grid-cols-4 gap-4">
          <Stat label="Uptime" value={running && info.started_at ? formatUptime(info.started_at) : '—'} testId="runtime-uptime" />
          <Stat label="CPU" value={running && info.cpu_percent !== undefined ? `${info.cpu_percent.toFixed(1)}%` : '—'} testId="runtime-cpu" />
          <Stat label="Memory" value={memory} testId="runtime-memory" />
          <Stat label="Container" value={info.container_id ?? '—'} testId="runtime-container-id" />
        </dl>
        {memoryShare !== null && (
          <div className="mt-4 h-1.5 rounded-full bg-muted overflow-hidden" aria-hidden="true">
            <div className="h-full bg-primary" style={{ width: `${memoryShare}%` }} />
          </div>
        )}
        {info.image && <p className="mt-3 text-xs text-muted-foreground font-mono truncate">{info.image}</p>}
      </CardContent>
    </Card>
  );
};
