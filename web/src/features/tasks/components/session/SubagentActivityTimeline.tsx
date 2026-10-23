import { useEffect, useRef } from 'react';
import { Bot, RefreshCw } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Markdown } from '@/components/ui/markdown';
import type { SubagentStep } from '../../utils/subagentActivity';

// A subagent's steps as a timeline: what it ran and what it wrote, oldest first, following the newest while it runs.
export function SubagentActivityTimeline({ steps, running }: { steps: SubagentStep[]; running: boolean }) {
  const endRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (running) endRef.current?.scrollIntoView?.({ block: 'end' });
  }, [running, steps.length]);

  return (
    <ol className="space-y-2 font-sans" data-testid="subagent-activity">
      {steps.map((step) =>
        step.kind === 'tool' ? (
          <li key={step.key} className="flex items-start gap-2.5">
            <span
              className={`mt-1.5 size-2 shrink-0 rounded-full ${
                step.status === 'running' ? 'animate-pulse bg-sky-500' : step.status === 'error' ? 'bg-rose-500' : 'bg-emerald-500'
              }`}
            />
            <div className="min-w-0 flex-1 rounded-md border border-border/60 bg-muted/20 px-2.5 py-1.5">
              <div className="flex items-center gap-2">
                <Badge variant="outline" className="shrink-0 border-primary/20 bg-primary/10 px-1.5 py-0 font-mono text-[10px] text-primary">
                  {step.name}
                </Badge>
                <span className="truncate font-mono text-[11px] text-muted-foreground" title={step.detail}>
                  {step.detail}
                </span>
              </div>
            </div>
          </li>
        ) : (
          <li key={step.key} className="flex items-start gap-2.5">
            <Bot className="mt-1 size-3.5 shrink-0 text-purple-400" />
            <div className="min-w-0 flex-1 text-xs leading-relaxed text-foreground">
              <Markdown content={step.text} />
            </div>
          </li>
        ),
      )}
      {running && (
        <li className="flex items-center gap-2 text-xs text-muted-foreground">
          <RefreshCw className="size-3 animate-spin text-primary" />
          Subagent is working…
        </li>
      )}
      <div ref={endRef} />
    </ol>
  );
}
