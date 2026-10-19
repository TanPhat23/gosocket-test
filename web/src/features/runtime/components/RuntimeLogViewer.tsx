import React, { useEffect, useRef } from 'react';
import { useQuery } from '@tanstack/react-query';
import { runtimeLogsQueryOptions, LOG_TAIL_LINES } from '../queries';

const NEAR_BOTTOM_PX = 48;

// The container's recent output. It follows new lines unless the reader has scrolled up.
export const RuntimeLogViewer: React.FC<{ projectId: string }> = ({ projectId }) => {
  const { data: logs, error, isPending } = useQuery(runtimeLogsQueryOptions(projectId));
  const boxRef = useRef<HTMLPreElement>(null);
  const following = useRef(true);

  useEffect(() => {
    const box = boxRef.current;
    if (box && following.current) box.scrollTop = box.scrollHeight;
  }, [logs]);

  const onScroll = () => {
    const box = boxRef.current;
    if (box) following.current = box.scrollHeight - box.scrollTop - box.clientHeight < NEAR_BOTTOM_PX;
  };

  let body: string;
  if (isPending) body = 'Loading…';
  else if (error) body = error instanceof Error ? error.message : 'Could not read the container logs';
  else body = logs?.trim() ? logs : 'No output yet.';

  return (
    <section className="space-y-2" aria-label="Container logs">
      <h2 className="text-sm font-medium">
        Logs <span className="text-xs font-normal text-muted-foreground">last {LOG_TAIL_LINES} lines</span>
      </h2>
      <pre
        ref={boxRef}
        onScroll={onScroll}
        className="h-96 overflow-auto rounded-lg border border-border bg-card/60 p-3 text-xs font-mono leading-relaxed whitespace-pre-wrap break-all"
        data-testid="runtime-logs"
      >
        {body}
      </pre>
    </section>
  );
};
