import React from 'react';
import { useTask, useTaskStore } from '../store/useTaskStore';
import { useWorkspaceSettingsStore } from '../store/useWorkspaceSettingsStore';
import { Card } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Ban,
  Cpu,
  CornerDownRight,
  GitPullRequest,
  GitBranch,
} from 'lucide-react';
import { cn } from '@/lib/utils';

export interface TaskCardProps {
  id: string;
}

export const TaskCard: React.FC<TaskCardProps> = ({ id }) => {
  const task = useTask(id);
  const selectTask = useTaskStore((state) => state.selectTask);
  const cancelTask = useTaskStore((state) => state.cancelTask);
  const connectedRepos = useWorkspaceSettingsStore((state) => state.connectedRepos);

  if (!task) {
    return null;
  }

  const repo = connectedRepos.find((r) => r.id === task.integration_id);
  const prLink = task.pull_request_url || task.pr_url;
  const isLive = task.status === 'in_progress';
  const isCancellable = ['backlog', 'queued', 'in_progress', 'review'].includes(task.status);

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

  const handleCardClick = () => {
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
        'group cursor-pointer border border-border/70 bg-card hover:border-primary/50 transition-all shadow-xs hover:shadow-sm overflow-hidden flex flex-col justify-between p-3 gap-2.5 rounded-lg',
        isLive && 'ring-1 ring-primary/40'
      )}
      data-testid={`task-card-${task.id}`}
    >
      {/* Top Header Row: Badges, Status, and ID */}
      <div className="flex items-center justify-between gap-1.5">
        <div className="flex items-center gap-1.5 min-w-0 flex-wrap">
          {getPriorityBadge(task.priority)}

          {task.parent_task_id && (
            <Badge
              variant="outline"
              className="font-mono text-[9px] text-muted-foreground bg-muted/30 border-dashed gap-1 px-1.5 py-0 h-4"
              title={`Subtask of parent task #${task.parent_task_id.slice(0, 8)}`}
            >
              <CornerDownRight className="size-2 text-muted-foreground" />
              Subtask
            </Badge>
          )}

          <span className="text-[11px] font-mono text-muted-foreground truncate" title={task.id}>
            #{task.id.slice(0, 8)}
          </span>

          {/* GitHub Issue badge */}
          {task.external_url ? (
            <a
              href={task.external_url}
              target="_blank"
              rel="noopener noreferrer"
              onClick={(e) => e.stopPropagation()}
              className="inline-flex items-center gap-1 font-mono text-[9px] text-muted-foreground bg-muted/50 hover:bg-muted hover:text-foreground border border-border/70 rounded px-1.5 py-0 h-4 transition-colors"
              title={`GitHub Issue ${task.external_id ? `#${task.external_id}` : ''}`}
              data-testid={`task-gh-issue-${task.id}`}
            >
              <span>GH #{task.external_id || 'issue'}</span>
            </a>
          ) : task.external_id ? (
            <span
              className="inline-flex items-center gap-1 font-mono text-[9px] text-muted-foreground bg-muted/50 border border-border/70 rounded px-1.5 py-0 h-4"
              title={`GitHub Issue #${task.external_id}`}
            >
              <span>GH #{task.external_id}</span>
            </span>
          ) : null}

          {/* PR badge */}
          {prLink && (
            <a
              href={prLink}
              target="_blank"
              rel="noopener noreferrer"
              onClick={(e) => e.stopPropagation()}
              className="inline-flex items-center gap-1 font-mono text-[9px] text-emerald-600 dark:text-emerald-400 bg-emerald-500/15 hover:bg-emerald-500/25 border border-emerald-500/30 rounded px-1.5 py-0 h-4 transition-colors font-medium"
              title={`Pull Request: ${prLink}`}
              data-testid={`task-pr-badge-${task.id}`}
            >
              <GitPullRequest className="size-2.5 shrink-0" />
              <span>PR</span>
            </a>
          )}
        </div>

        {/* Live indicator or status */}
        <div className="shrink-0">
          {isLive ? (
            <span className="flex items-center gap-1 text-[10px] font-medium text-emerald-600 dark:text-emerald-400 font-mono">
              <span className="size-1.5 rounded-full bg-emerald-500 animate-pulse" />
              live
            </span>
          ) : (
            <span className="text-[10px] text-muted-foreground/70 font-mono">
              {task.status}
            </span>
          )}
        </div>
      </div>

      {/* Task Title */}
      <h3 className="text-xs sm:text-[13px] font-semibold text-foreground line-clamp-2 leading-snug group-hover:text-primary transition-colors">
        {task.title}
      </h3>

      {/* Bottom Metadata: Repo tag, Model, and Skills count */}
      <div className="flex items-center justify-between gap-2 text-[10px] text-muted-foreground pt-1 border-t border-border/40 font-mono">
        <div className="flex items-center gap-2 min-w-0 flex-1 truncate">
          {repo ? (
            <span className="inline-flex items-center gap-1 text-primary truncate font-medium" title={`Repository: ${repo.external_id}`}>
              <GitBranch className="size-2.5 shrink-0" />
              <span className="truncate">{repo.external_id}</span>
            </span>
          ) : (
            task.model && (
              <span className="inline-flex items-center gap-1 truncate text-muted-foreground/80">
                <Cpu className="size-2.5 shrink-0 text-muted-foreground" />
                <span className="truncate">{task.model.split('/').pop()}</span>
              </span>
            )
          )}

          {task.skills && task.skills.length > 0 && (
            <span className="text-[9px] text-muted-foreground/60 hidden sm:inline">
              · {task.skills.length} skills
            </span>
          )}
        </div>

        {isCancellable && (
          <Button
            size="xs"
            variant="ghost"
            onClick={handleCancel}
            className="h-5 px-1.5 text-[10px] text-muted-foreground hover:text-destructive opacity-0 group-hover:opacity-100 transition-opacity"
            title="Cancel task"
          >
            <Ban className="size-2.5 mr-1" />
            Cancel
          </Button>
        )}
      </div>
    </Card>
  );
};
