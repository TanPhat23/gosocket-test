import React from 'react';
import { useTask, useTaskStore } from '../store/useTaskStore';
import { Card, CardHeader, CardContent, CardFooter } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Eye, GitCompare, Ban, Cpu, Wrench, Server } from 'lucide-react';
import { cn } from '@/lib/utils';

export interface TaskCardProps {
  id: string;
}

export const TaskCard: React.FC<TaskCardProps> = ({ id }) => {
  const task = useTask(id);
  const selectTask = useTaskStore((state) => state.selectTask);
  const cancelTask = useTaskStore((state) => state.cancelTask);

  if (!task) {
    return null;
  }

  // Priority mapping
  // Critical (> 80 or label): red/destructive
  // High (> 60): amber
  // Normal (> 30): blue/secondary
  // Low (<= 30): slate/outline
  const getPriorityBadge = (priority: number) => {
    if (priority >= 80) {
      return (
        <Badge variant="destructive" className="font-mono text-[10px] uppercase font-semibold">
          Critical
        </Badge>
      );
    }
    if (priority >= 60) {
      return (
        <Badge
          className="bg-amber-500/15 text-amber-600 dark:text-amber-400 border border-amber-500/30 font-mono text-[10px] uppercase font-semibold hover:bg-amber-500/20"
        >
          High
        </Badge>
      );
    }
    if (priority >= 30) {
      return (
        <Badge variant="secondary" className="font-mono text-[10px] uppercase font-semibold">
          Normal
        </Badge>
      );
    }
    return (
      <Badge variant="outline" className="font-mono text-[10px] uppercase text-muted-foreground">
        Low
      </Badge>
    );
  };

  const isLive = task.status === 'in_progress';
  const isCancellable = ['backlog', 'queued', 'in_progress', 'review'].includes(task.status);

  const handleCardClick = () => {
    selectTask(task.id);
  };

  const handleInspect = (e: React.MouseEvent) => {
    e.stopPropagation();
    selectTask(task.id);
  };

  const handleDiff = (e: React.MouseEvent) => {
    e.stopPropagation();
    selectTask(task.id);
  };

  const handleCancel = async (e: React.MouseEvent) => {
    e.stopPropagation();
    await cancelTask(task.id);
  };

  return (
    <Card
      onClick={handleCardClick}
      className={cn(
        'group cursor-pointer border border-border/80 bg-card hover:border-primary/50 transition-all shadow-xs hover:shadow-sm overflow-hidden flex flex-col justify-between',
        isLive && 'ring-1 ring-primary/40'
      )}
      data-testid={`task-card-${task.id}`}
    >
      <CardHeader className="p-3.5 pb-2 flex flex-col gap-2">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-1.5 min-w-0">
            {getPriorityBadge(task.priority)}
            <span className="text-[11px] font-mono text-muted-foreground truncate" title={task.id}>
              #{task.id.slice(0, 8)}
            </span>
          </div>

          {/* Status badge with live pulse */}
          <div className="flex items-center gap-1.5 shrink-0">
            {isLive ? (
              <span className="flex items-center gap-1.5 text-[11px] font-medium text-emerald-600 dark:text-emerald-400">
                <span className="relative flex size-2">
                  <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                  <span className="relative inline-flex rounded-full size-2 bg-emerald-500" />
                </span>
                in_progress
              </span>
            ) : (
              <span className="text-[11px] text-muted-foreground font-mono">
                {task.status}
              </span>
            )}
          </div>
        </div>

        <h3 className="text-sm font-semibold text-foreground line-clamp-2 leading-snug group-hover:text-primary transition-colors">
          {task.title}
        </h3>
      </CardHeader>

      <CardContent className="p-3.5 pt-0 pb-2 space-y-2">
        {/* Model info if present */}
        {task.model && (
          <div className="flex items-center gap-1 text-[11px] text-muted-foreground font-mono">
            <Cpu className="size-3 text-muted-foreground shrink-0" />
            <span className="truncate">{task.model}</span>
          </div>
        )}

        {/* Skills Chips */}
        {task.skills && task.skills.length > 0 && (
          <div className="flex flex-wrap items-center gap-1">
            <Wrench className="size-3 text-muted-foreground shrink-0" />
            {task.skills.map((skill) => (
              <Badge
                key={skill}
                variant="outline"
                className="text-[10px] py-0 px-1.5 font-normal bg-muted/40"
              >
                {skill}
              </Badge>
            ))}
          </div>
        )}

        {/* MCP chips */}
        {task.mcp_servers && task.mcp_servers.length > 0 && (
          <div className="flex flex-wrap items-center gap-1">
            <Server className="size-3 text-muted-foreground shrink-0" />
            {task.mcp_servers.map((mcp) => (
              <Badge
                key={mcp}
                variant="secondary"
                className="text-[10px] py-0 px-1.5 font-normal"
              >
                {mcp}
              </Badge>
            ))}
          </div>
        )}
      </CardContent>

      <CardFooter className="p-2.5 px-3.5 pt-2 border-t border-border/50 bg-muted/20 flex items-center justify-between gap-1">
        <div className="flex items-center gap-1">
          <Button
            size="xs"
            variant="ghost"
            onClick={handleInspect}
            className="h-6 text-xs text-muted-foreground hover:text-foreground"
            title="Inspect task details and logs"
          >
            <Eye className="size-3 mr-1" />
            Inspect
          </Button>

          <Button
            size="xs"
            variant="ghost"
            onClick={handleDiff}
            className="h-6 text-xs text-muted-foreground hover:text-foreground"
            title="Inspect code diff"
          >
            <GitCompare className="size-3 mr-1" />
            Diff
          </Button>
        </div>

        {isCancellable && (
          <Button
            size="xs"
            variant="ghost"
            onClick={handleCancel}
            className="h-6 text-xs text-muted-foreground hover:text-destructive hover:bg-destructive/10"
            title="Cancel execution"
          >
            <Ban className="size-3 mr-1" />
            Cancel
          </Button>
        )}
      </CardFooter>
    </Card>
  );
};
