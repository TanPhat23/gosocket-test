import React, { useEffect, useId } from 'react';
import { useTaskStore } from '../store/useTaskStore';
import { useWorkspaceSettingsStore } from '../store/useWorkspaceSettingsStore';
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
  SheetFooter,
} from '@/components/ui/sheet';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  GitPullRequest,
  GitBranch,
  ChevronLeft,
  ChevronRight,
  ExternalLink,
  Cpu,
  Layers,
  Wrench,
  CornerDownRight,
} from 'lucide-react';
import { cn } from '@/lib/utils';

export interface TaskPeekSheetProps {
  taskId: string | null;
  taskIds: string[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onTaskChange: (taskId: string) => void;
  onOpenInspector: (taskId: string) => void;
  finalFocus: () => HTMLElement | null;
}

const isEditableTarget = (target: EventTarget | null) =>
  target instanceof HTMLElement &&
  (target.isContentEditable || target.matches('input, textarea, select, [role="textbox"]'));

export const TaskPeekSheet: React.FC<TaskPeekSheetProps> = ({
  taskId,
  taskIds,
  open,
  onOpenChange,
  onTaskChange,
  onOpenInspector,
  finalFocus,
}) => {
  const task = useTaskStore((state) => (taskId ? state.tasks[taskId] : undefined));
  const connectedRepos = useWorkspaceSettingsStore((state) => state.connectedRepos);
  const liveRegionId = useId();

  // If open but task is null or removed from store, close peek
  useEffect(() => {
    if (open && taskId && !task) {
      onOpenChange(false);
    }
  }, [open, taskId, task, onOpenChange]);

  const index = taskId ? taskIds.indexOf(taskId) : -1;
  const previousTaskId = index > 0 ? taskIds[index - 1] : null;
  const nextTaskId = index >= 0 && index < taskIds.length - 1 ? taskIds[index + 1] : null;

  // Keyboard navigation scoped to open peek: j (next), k (previous)
  useEffect(() => {
    if (!open) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (isEditableTarget(e.target)) return;

      if (e.key === 'j' || e.key === 'ArrowDown') {
        if (nextTaskId) {
          e.preventDefault();
          onTaskChange(nextTaskId);
        }
      } else if (e.key === 'k' || e.key === 'ArrowUp') {
        if (previousTaskId) {
          e.preventDefault();
          onTaskChange(previousTaskId);
        }
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [open, nextTaskId, previousTaskId, onTaskChange]);

  if (!task && !open) {
    return null;
  }

  const repo = task ? connectedRepos.find((r) => r.id === task.integration_id) : undefined;
  const prLink = task?.pull_request_url || task?.pr_url;

  const getPriorityBadge = (priority: number) => {
    if (priority >= 80) {
      return (
        <Badge variant="destructive" className="font-mono text-[9px] uppercase font-semibold px-1.5 py-0 h-4">
          Critical
        </Badge>
      );
    }
    if (priority >= 60) {
      return (
        <Badge className="bg-amber-500/15 text-amber-600 dark:text-amber-400 border border-amber-500/30 font-mono text-[9px] uppercase font-semibold hover:bg-amber-500/20 px-1.5 py-0 h-4">
          High
        </Badge>
      );
    }
    if (priority >= 30) {
      return (
        <Badge variant="secondary" className="font-mono text-[9px] uppercase font-semibold px-1.5 py-0 h-4">
          Normal
        </Badge>
      );
    }
    return (
      <Badge variant="outline" className="font-mono text-[9px] uppercase text-muted-foreground px-1.5 py-0 h-4">
        Low
      </Badge>
    );
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className={cn(
          'data-[side=right]:w-full data-[side=right]:max-w-full data-[side=right]:sm:max-w-md data-[side=right]:md:max-w-lg data-[side=right]:lg:max-w-xl h-full p-0 flex flex-col gap-0 border-l border-border bg-card shadow-2xl',
          'motion-reduce:transition-none'
        )}
        finalFocus={() => {
          const target = finalFocus();
          return target && target.isConnected ? target : true;
        }}
      >
        {/* Accessible live status region for screen readers */}
        <div id={liveRegionId} role="status" aria-live="polite" className="sr-only">
          {task ? `Previewing task ${task.title}, status: ${task.status}` : ''}
        </div>

        {/* Sheet Top Navigation Bar */}
        <div className="flex items-center justify-between px-4 py-2.5 border-b border-border bg-muted/30">
          <div className="flex items-center gap-1">
            <Button
              type="button"
              variant="outline"
              size="icon-xs"
              onClick={() => previousTaskId && onTaskChange(previousTaskId)}
              disabled={!previousTaskId}
              aria-label="Previous task in lane (k)"
              title="Previous task (k)"
            >
              <ChevronLeft className="size-3.5" />
            </Button>
            <Button
              type="button"
              variant="outline"
              size="icon-xs"
              onClick={() => nextTaskId && onTaskChange(nextTaskId)}
              disabled={!nextTaskId}
              aria-label="Next task in lane (j)"
              title="Next task (j)"
            >
              <ChevronRight className="size-3.5" />
            </Button>
            <span className="text-[11px] font-mono text-muted-foreground ml-2">
              {index >= 0 ? `${index + 1} of ${taskIds.length}` : 'Task moved'}
            </span>
          </div>

          <div className="pr-7 text-xs font-mono text-muted-foreground">
            {taskId && `#${taskId.slice(0, 8)}`}
          </div>
        </div>

        {task && (
          <>
            <SheetHeader className="p-5 pb-3 gap-2">
              <div className="flex items-center gap-2 flex-wrap">
                {getPriorityBadge(task.priority)}
                <Badge variant="outline" className="font-mono text-[9px] uppercase px-1.5 py-0 h-4">
                  {task.status}
                </Badge>
                {task.parent_task_id && (
                  <Badge
                    variant="outline"
                    className="font-mono text-[9px] text-muted-foreground bg-muted/30 border-dashed gap-1 px-1.5 py-0 h-4"
                  >
                    <CornerDownRight className="size-2 text-muted-foreground" />
                    Subtask
                  </Badge>
                )}
              </div>
              <SheetTitle className="text-base sm:text-lg font-semibold text-foreground leading-snug">
                {task.title}
              </SheetTitle>
              <SheetDescription className="sr-only">
                Quick preview of task {task.title} with navigation and inspect action.
              </SheetDescription>
            </SheetHeader>

            {/* Scrollable Sheet Body */}
            <div className="flex-1 overflow-y-auto px-5 py-2 space-y-5">
              {/* Context row: Repo / Branch, PR, External Issue */}
              <div className="flex flex-wrap items-center gap-2 font-mono text-xs text-muted-foreground">
                {repo && (
                  <span className="inline-flex items-center gap-1.5 text-primary bg-primary/10 px-2 py-0.5 rounded border border-primary/20 font-medium">
                    <GitBranch className="size-3 shrink-0" />
                    <span>{repo.external_id}</span>
                  </span>
                )}
                {prLink && (
                  <a
                    href={prLink}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 font-mono text-xs text-emerald-600 dark:text-emerald-400 bg-emerald-500/15 hover:bg-emerald-500/25 border border-emerald-500/30 rounded px-2 py-0.5 transition-colors font-medium"
                  >
                    <GitPullRequest className="size-3 shrink-0" />
                    <span>PR</span>
                    <ExternalLink className="size-2.5 ml-0.5 opacity-70" />
                  </a>
                )}
                {task.external_url ? (
                  <a
                    href={task.external_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 font-mono text-xs text-muted-foreground bg-muted/60 hover:bg-muted hover:text-foreground border border-border rounded px-2 py-0.5 transition-colors"
                  >
                    <span>Issue #{task.external_id || 'link'}</span>
                    <ExternalLink className="size-2.5 ml-0.5 opacity-70" />
                  </a>
                ) : task.external_id ? (
                  <span className="inline-flex items-center gap-1 font-mono text-xs text-muted-foreground bg-muted/60 border border-border rounded px-2 py-0.5">
                    Issue #{task.external_id}
                  </span>
                ) : null}
              </div>

              {/* Model & Assignee info */}
              {(task.model || task.assignee_id) && (
                <div className="grid grid-cols-2 gap-2 text-xs font-mono bg-muted/30 p-2.5 rounded-lg border border-border/50">
                  {task.model && (
                    <div className="flex items-center gap-1.5 text-muted-foreground">
                      <Cpu className="size-3.5 shrink-0 text-muted-foreground/70" />
                      <span className="truncate" title={task.model}>
                        {task.model.split('/').pop()}
                      </span>
                    </div>
                  )}
                  {task.assignee_id && (
                    <div className="flex items-center gap-1.5 text-muted-foreground">
                      <span className="text-[10px] uppercase font-semibold text-muted-foreground/60">
                        {task.assignee_type}:
                      </span>
                      <span className="truncate" title={task.assignee_id}>
                        {task.assignee_id}
                      </span>
                    </div>
                  )}
                </div>
              )}

              {/* Prompt preview */}
              <div className="space-y-1.5">
                <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                  Prompt
                </span>
                <div className="p-3 rounded-lg bg-muted/40 border border-border/60 text-xs font-mono whitespace-pre-wrap break-words leading-relaxed max-h-64 overflow-y-auto">
                  {task.prompt}
                </div>
              </div>

              {/* Skills and MCP Servers */}
              {((task.skills && task.skills.length > 0) || (task.mcp_servers && task.mcp_servers.length > 0)) && (
                <div className="space-y-3 pt-2 border-t border-border/50">
                  {task.skills && task.skills.length > 0 && (
                    <div>
                      <div className="flex items-center gap-1.5 text-[11px] font-semibold uppercase text-muted-foreground mb-1.5">
                        <Wrench className="size-3" />
                        Skills ({task.skills.length})
                      </div>
                      <div className="flex flex-wrap gap-1.5">
                        {task.skills.map((skill: string) => (
                          <Badge key={skill} variant="secondary" className="font-mono text-[10px] px-1.5 py-0 h-4">
                            {skill}
                          </Badge>
                        ))}
                      </div>
                    </div>
                  )}

                  {task.mcp_servers && task.mcp_servers.length > 0 && (
                    <div>
                      <div className="flex items-center gap-1.5 text-[11px] font-semibold uppercase text-muted-foreground mb-1.5">
                        <Layers className="size-3" />
                        MCP Servers ({task.mcp_servers.length})
                      </div>
                      <div className="flex flex-wrap gap-1.5">
                        {task.mcp_servers.map((server: string) => (
                          <Badge key={server} variant="outline" className="font-mono text-[10px] px-1.5 py-0 h-4">
                            {server}
                          </Badge>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              )}
            </div>

            {/* Footer actions */}
            <SheetFooter className="p-4 border-t border-border bg-muted/20 flex flex-row items-center justify-between gap-2">
              <span className="text-[11px] text-muted-foreground font-mono hidden sm:inline">
                Press <kbd className="px-1 py-0.5 rounded bg-muted border border-border text-[10px]">j</kbd>/
                <kbd className="px-1 py-0.5 rounded bg-muted border border-border text-[10px]">k</kbd> to navigate
              </span>
              <Button
                type="button"
                variant="default"
                size="sm"
                className="w-full sm:w-auto ml-auto font-medium"
                onClick={() => {
                  onOpenChange(false);
                  onOpenInspector(task.id);
                }}
              >
                Open full inspector
              </Button>
            </SheetFooter>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
};
