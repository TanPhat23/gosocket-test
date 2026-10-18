import React, { lazy, Suspense, useState } from 'react';
import { useSelectedTask, useTaskStore } from '../store/useTaskStore';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { DiffTab } from './inspector/DiffTab';
import { TaskInspectorProvider } from './inspector/TaskInspectorContext';
import { usePullRequestController } from './inspector/usePullRequestController';
import { FileExplorer } from './FileExplorer';
import { AgentSessionView } from './AgentSessionView';
import { Markdown } from '@/components/ui/markdown';
import {
  Info,
  Terminal as TerminalIcon,
  GitCompare,
  Calendar,
  Clock,
  Box,
  Layers,
  Cpu,
  User,
  GitBranch,
  CornerDownRight,
  ChevronRight,
  GitPullRequest,
  ExternalLink,
  CheckCircle,
  AlertCircle,
  Loader2,
  Copy,
  Check,
  Bot,
  GitMerge,
  RefreshCw,
  Database,
  DollarSign,
  FolderTree,
  Zap,
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { taskApi } from '../api/taskApi';
import { taskSessionQueryOptions } from '../queries';
import type { TaskStatus } from '../schemas/taskSchemas';
import { cn } from '@/lib/utils';

// xterm is the heaviest part of the inspector. It loads when the terminal tab is first opened.
const TerminalView = lazy(() =>
  import('./TerminalView').then((m) => ({ default: m.TerminalView }))
);

const getStatusBadge = (status: TaskStatus) => {
  switch (status) {
    case 'in_progress':
      return (
        <Badge className="bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border-emerald-500/30 text-[10px] font-mono">
          in_progress
        </Badge>
      );
    case 'done':
      return (
        <Badge variant="secondary" className="text-[10px] font-mono">
          done
        </Badge>
      );
    case 'failed':
      return (
        <Badge variant="destructive" className="text-[10px] font-mono">
          failed
        </Badge>
      );
    case 'cancelled':
      return (
        <Badge variant="outline" className="text-muted-foreground text-[10px] font-mono">
          cancelled
        </Badge>
      );
    case 'review':
      return (
        <Badge className="bg-amber-500/15 text-amber-600 dark:text-amber-400 border-amber-500/30 text-[10px] font-mono">
          review
        </Badge>
      );
    default:
      return (
        <Badge variant="outline" className="text-[10px] font-mono">
          {status}
        </Badge>
      );
  }
};

interface TaskInspectorModalProps {
  taskId?: string | null;
  onClose?: () => void;
}

export const TaskInspectorModal: React.FC<TaskInspectorModalProps> = ({
  taskId: propTaskId,
  onClose,
}) => {
  const selectedTask = useSelectedTask();
  const selectTask = useTaskStore((state) => state.selectTask);
  const tasks = useTaskStore((state) => state.tasks);

  const task = propTaskId !== undefined
    ? (propTaskId ? tasks[propTaskId] ?? null : null)
    : selectedTask;

  // The list payload carries no failure reason; the single-task read adds it for failed tasks.
  const failedTaskId = task?.status === 'failed' ? task.id : '';
  const [failure, setFailure] = useState<{ id: string; reason: string } | null>(null);
  React.useEffect(() => {
    if (!failedTaskId) return;
    let cancelled = false;
    taskApi
      .getTask(failedTaskId)
      .then((detail) => {
        if (!cancelled && detail.failure_reason) {
          setFailure({ id: failedTaskId, reason: detail.failure_reason });
        }
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [failedTaskId]);
  const failureReason = failure?.id === failedTaskId ? failure.reason : null;

  const [activeTab, setActiveTab] = useState<string>('overview');

  const [copiedSessionId, setCopiedSessionId] = useState(false);
  const pullRequest = usePullRequestController(task);
  const { isCreatingPR, isSyncingPR, prError, prToast, handleCreatePR, handleSyncPR } = pullRequest;
  // Shares the task-session query with the Agent Session tab, so opening the inspector fetches the session once.
  const { data: sessionData } = useQuery({
    ...taskSessionQueryOptions(task?.id ?? ''),
    enabled: Boolean(task?.id),
    retry: false,
  });

  const isOpen = !!task;

  const handleCopySessionId = async () => {
    if (!task?.session_id) return;
    try {
      await navigator.clipboard.writeText(task.session_id);
      setCopiedSessionId(true);
      setTimeout(() => setCopiedSessionId(false), 2000);
    } catch {
      // fallback or ignore clipboard write error
    }
  };

  const childTasks = React.useMemo(() => {
    if (!task) return [];
    return Object.values(tasks).filter((t) => t.parent_task_id === task.id);
  }, [tasks, task]);

  const handleOpenChange = (open: boolean) => {
    if (!open) {
      if (onClose) {
        onClose();
      } else {
        selectTask(null);
      }
    }
  };

  if (!task) return null;

  const prLink = task ? task.pull_request_url || task.pr_url : null;
  const isCompleted = task ? ['done', 'review'].includes(task.status) : false;

  const formatDate = (isoString: string) => {
    try {
      return new Date(isoString).toLocaleString();
    } catch {
      return isoString;
    }
  };

  return (
    <TaskInspectorProvider task={task} pullRequest={pullRequest}>
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogContent className="w-full h-full max-h-screen rounded-none p-3 sm:rounded-xl sm:p-6 sm:w-[96vw] sm:max-w-[1680px] sm:h-[92vh] sm:max-h-[94vh] flex flex-col">
        <DialogHeader className="pb-3 border-b border-border">
          <div className="flex items-center justify-between gap-4 pr-6">
            <div className="flex items-center gap-2">
              <span className="text-xs font-mono text-muted-foreground">
                #{task.id.slice(0, 8)}
              </span>
              <DialogTitle className="text-base font-semibold truncate max-w-lg">
                {task.title}
              </DialogTitle>
            </div>
            <div className="flex items-center gap-2 shrink-0">
              {/* GitHub Issue Link */}
              {task.external_url ? (
                <a
                  href={task.external_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 text-xs font-mono text-muted-foreground hover:text-foreground bg-muted/60 border border-border/80 rounded px-2 py-0.5 transition-colors"
                  title="View GitHub Issue"
                  data-testid="inspector-gh-issue-link"
                >
                  <GitBranch className="size-3" />
                  <span>Issue #{task.external_id || ''}</span>
                  <ExternalLink className="size-2.5" />
                </a>
              ) : task.external_id ? (
                <Badge variant="outline" className="text-xs font-mono gap-1">
                  <GitBranch className="size-3" />
                  <span>Issue #{task.external_id}</span>
                </Badge>
              ) : null}

              <Badge variant="outline" className="font-mono text-xs capitalize">
                {task.status}
              </Badge>
              <Badge variant="secondary" className="font-mono text-xs">
                P{task.priority}
              </Badge>
            </div>
          </div>
        </DialogHeader>

        {/* PR Action Notification / Toast */}
        {prToast && (
          <div
            className="flex items-center justify-between gap-3 p-3 text-xs bg-emerald-500/15 border border-emerald-500/30 rounded-lg text-emerald-700 dark:text-emerald-300 animate-in fade-in-50"
            data-testid="pr-success-toast"
          >
            <div className="flex items-center gap-2">
              <CheckCircle className="size-4 shrink-0 text-emerald-600 dark:text-emerald-400" />
              <span className="font-medium">{prToast.message}</span>
            </div>
            {prToast.url && (
              <a
                href={prToast.url}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1 font-mono text-xs underline font-semibold text-emerald-700 dark:text-emerald-300 hover:text-emerald-900"
              >
                Open PR
                <ExternalLink className="size-3" />
              </a>
            )}
          </div>
        )}

        {/* PR Error Notification */}
        {prError && (
          <div
            className="flex items-center gap-2 p-3 text-xs bg-destructive/10 border border-destructive/20 rounded-lg text-destructive"
            data-testid="pr-error-banner"
          >
            <AlertCircle className="size-4 shrink-0" />
            <span className="font-medium">{prError}</span>
          </div>
        )}

        {/* Actions Bar for Tasks (Only shown when PR error or PR action is available) */}
        {(prError || (isCompleted && !prLink) || prLink) && (
          <div className="flex items-center justify-between gap-3 p-2.5 rounded-lg border border-border bg-muted/20">
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <span className="font-medium text-foreground">Task Workflow:</span>
              <span>Status is <strong className="text-foreground">{task.status}</strong></span>
            </div>

            <div className="flex items-center gap-2">
              {/* PR Action: completed and no PR yet */}
              {isCompleted && !prLink && (
                <Button
                  size="sm"
                  onClick={handleCreatePR}
                  disabled={isCreatingPR}
                  className="h-7 text-xs gap-1.5 font-medium bg-emerald-600 hover:bg-emerald-700 text-white"
                  data-testid="create-pr-btn"
                >
                  {isCreatingPR ? (
                    <>
                      <Loader2 className="size-3 animate-spin" />
                      Creating PR...
                    </>
                  ) : (
                    <>
                      <GitPullRequest className="size-3" />
                      Create Pull Request
                    </>
                  )}
                </Button>
              )}

              {prLink && task.status === 'done' && (
                <a
                  href={prLink}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1.5 h-7 px-3 text-xs font-mono rounded-md bg-purple-500/15 text-purple-600 dark:text-purple-400 border border-purple-500/30 hover:bg-purple-500/25 transition-colors font-medium"
                  data-testid="view-pr-btn"
                >
                  <GitMerge className="size-3" />
                  PR Merged ({prLink.split('/').slice(-2).join('/') || 'Link'})
                  <ExternalLink className="size-2.5" />
                </a>
              )}

              {prLink && task.status !== 'done' && (
                <>
                  <a
                    href={prLink}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1.5 h-7 px-3 text-xs font-mono rounded-md bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border border-emerald-500/30 hover:bg-emerald-500/25 transition-colors font-medium"
                    data-testid="view-pr-btn"
                  >
                    <GitPullRequest className="size-3" />
                    View PR ({prLink.split('/').slice(-2).join('/') || 'Link'})
                    <ExternalLink className="size-2.5" />
                  </a>

                  <Button
                    size="sm"
                    variant="outline"
                    onClick={handleSyncPR}
                    disabled={isSyncingPR}
                    className="h-7 text-xs gap-1.5 font-medium border-border hover:bg-muted"
                    title="Check if PR has been merged on GitHub"
                    data-testid="sync-pr-btn"
                  >
                    <RefreshCw className={cn('size-3', isSyncingPR && 'animate-spin')} />
                    {isSyncingPR ? 'Checking...' : 'Sync PR Status'}
                  </Button>
                </>
              )}
            </div>
          </div>
        )}

        <Tabs
          value={activeTab}
          onValueChange={(val) => setActiveTab(val as string)}
          className="flex-1 flex flex-col mt-3 overflow-hidden min-h-0"
        >
          <div className="overflow-x-auto pb-1 max-w-full">
            <TabsList className="inline-flex min-w-full sm:min-w-0 sm:grid sm:grid-cols-5 sm:w-full sm:max-w-2xl">
              <TabsTrigger value="overview" className="text-xs flex items-center gap-1.5 shrink-0 sm:shrink">
                <Info className="size-3.5" />
                Overview
              </TabsTrigger>
              <TabsTrigger value="session" className="text-xs flex items-center gap-1.5 shrink-0 sm:shrink" data-testid="tab-agent-session">
                <Bot className="size-3.5" />
                Agent Session
              </TabsTrigger>
              <TabsTrigger value="terminal" className="text-xs flex items-center gap-1.5 shrink-0 sm:shrink">
                <TerminalIcon className="size-3.5" />
                Terminal Logs
              </TabsTrigger>
              <TabsTrigger value="diff" className="text-xs flex items-center gap-1.5 shrink-0 sm:shrink">
                <GitCompare className="size-3.5" />
                Git Diff Review
              </TabsTrigger>
              <TabsTrigger value="files" className="text-xs flex items-center gap-1.5 shrink-0 sm:shrink" data-testid="tab-file-explorer">
                <FolderTree className="size-3.5" />
                File Explorer
              </TabsTrigger>
            </TabsList>
          </div>

          {/* Overview Tab */}
          <TabsContent value="overview" className="flex-1 overflow-y-auto mt-4 space-y-4 pr-1">
            {failureReason && (
              <div role="alert" className="p-3 rounded-lg border border-destructive/30 bg-destructive/10 text-xs text-destructive">
                <span className="font-semibold">Failure reason: </span>
                {failureReason}
              </div>
            )}
            {/* Prompt block */}
            <div className="space-y-1.5">
              <h4 className="text-xs font-semibold text-foreground uppercase tracking-wider">
                Prompt &amp; Specification
              </h4>
              <div className="p-3 rounded-lg border border-border bg-muted/30 text-xs leading-relaxed text-foreground">
                <Markdown content={task.prompt} />
              </div>
            </div>

            {/* Metadata Grid */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3 pt-2">
              <div className="p-3 rounded-lg border border-border bg-card space-y-2">
                <h5 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                  Execution Runtime
                </h5>
                <div className="space-y-1 text-xs">
                  <div className="flex items-center justify-between">
                    <span className="text-muted-foreground flex items-center gap-1.5">
                      <Cpu className="size-3.5" /> Model Engine:
                    </span>
                    <span className="font-mono">{task.model || 'google/gemini-3.8-flash'}</span>
                  </div>
                  <div className="flex items-center justify-between">
                    <span className="text-muted-foreground flex items-center gap-1.5">
                      <User className="size-3.5" /> Assignee:
                    </span>
                    <span className="font-mono">{task.assignee_id} ({task.assignee_type})</span>
                  </div>
                  <div className="flex items-center justify-between">
                    <span className="text-muted-foreground flex items-center gap-1.5">
                      <Box className="size-3.5" /> Container Sandbox:
                    </span>
                    <span className="font-mono text-emerald-600 dark:text-emerald-400">atm-sandbox-{task.id.slice(0, 6)}</span>
                  </div>
                  <div className="flex items-center justify-between">
                    <span className="text-muted-foreground flex items-center gap-1.5">
                      <Layers className="size-3.5" /> Isolated Worktree:
                    </span>
                    <span className="font-mono">worktrees/{task.id.slice(0, 8)}</span>
                  </div>
                  {task.session_id && (
                    <div className="flex items-center justify-between gap-2 pt-1 border-t border-border/40">
                      <span className="text-muted-foreground flex items-center gap-1.5 shrink-0">
                        <TerminalIcon className="size-3.5" /> OpenCode Session:
                      </span>
                      <div className="flex items-center gap-1 min-w-0">
                        <Badge
                          variant="secondary"
                          className="font-mono text-[11px] truncate max-w-[180px] select-all font-normal"
                          title={task.session_id}
                          data-testid="opencode-session-id"
                        >
                          {task.session_id}
                        </Badge>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          onClick={handleCopySessionId}
                          className="h-6 w-6 shrink-0 text-muted-foreground hover:text-foreground"
                          title={copiedSessionId ? 'Copied' : 'Copy Session ID'}
                          aria-label={copiedSessionId ? 'Copied OpenCode Session ID' : 'Copy OpenCode Session ID'}
                        >
                          {copiedSessionId ? (
                            <Check className="size-3 text-emerald-500" />
                          ) : (
                            <Copy className="size-3" />
                          )}
                        </Button>
                      </div>
                    </div>
                  )}
                  {task.parent_task_id && (
                    <div className="flex items-center justify-between pt-1 border-t border-border/40">
                      <span className="text-muted-foreground flex items-center gap-1.5">
                        <CornerDownRight className="size-3.5" /> Parent Task:
                      </span>
                      <Button
                        variant="link"
                        size="xs"
                        onClick={() => selectTask(task.parent_task_id!)}
                        className="h-auto p-0 font-mono text-xs text-primary hover:underline"
                        title={`Inspect parent task ${task.parent_task_id}`}
                      >
                        #{task.parent_task_id.slice(0, 8)}
                      </Button>
                    </div>
                  )}
                </div>
              </div>

              <div className="p-3 rounded-lg border border-border bg-card space-y-2">
                <h5 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                  Timestamps &amp; Progress
                </h5>
                <div className="space-y-1 text-xs">
                  <div className="flex items-center justify-between">
                    <span className="text-muted-foreground flex items-center gap-1.5">
                      <Calendar className="size-3.5" /> Created:
                    </span>
                    <span className="font-mono">{formatDate(task.created_at)}</span>
                  </div>
                  <div className="flex items-center justify-between">
                    <span className="text-muted-foreground flex items-center gap-1.5">
                      <Clock className="size-3.5" /> Last Updated:
                    </span>
                    <span className="font-mono">{formatDate(task.updated_at)}</span>
                  </div>
                  {task.heartbeat_at && (
                    <div className="flex items-center justify-between">
                      <span className="text-muted-foreground flex items-center gap-1.5">
                        <Clock className="size-3.5" /> Heartbeat:
                      </span>
                      <span className="font-mono text-emerald-600 dark:text-emerald-400">
                        {formatDate(task.heartbeat_at)}
                      </span>
                    </div>
                  )}
                </div>
              </div>
            </div>

            {/* Session & Token Metrics Card if Session exists */}
            {sessionData && (
              <div className="p-3 rounded-lg border border-border bg-card space-y-2.5">
                <div className="flex items-center justify-between">
                  <h5 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider flex items-center gap-1.5">
                    <Zap className="size-3.5 text-amber-500" />
                    Session Token Metrics &amp; Usage
                  </h5>
                  <Badge variant="outline" className="font-mono text-[10px] capitalize">
                    {sessionData.status || 'unknown'}
                  </Badge>
                </div>

                <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
                  <div className="p-2 rounded bg-muted/40 border border-border/40 font-mono">
                    <span className="text-[10px] text-muted-foreground block">Input Tokens</span>
                    <strong className="text-foreground text-xs font-semibold">
                      {sessionData.tokens?.input?.toLocaleString() ?? 0}
                    </strong>
                  </div>
                  <div className="p-2 rounded bg-muted/40 border border-border/40 font-mono">
                    <span className="text-[10px] text-muted-foreground block">Output Tokens</span>
                    <strong className="text-foreground text-xs font-semibold">
                      {sessionData.tokens?.output?.toLocaleString() ?? 0}
                    </strong>
                  </div>
                  <div className="p-2 rounded bg-muted/40 border border-border/40 font-mono">
                    <span className="text-[10px] text-muted-foreground flex items-center gap-1">
                      <Database className="size-2.5 text-emerald-500" /> Cache Read
                    </span>
                    <strong className="text-foreground text-xs font-semibold">
                      {sessionData.tokens?.cache_read?.toLocaleString() ?? 0}
                    </strong>
                  </div>
                  <div className="p-2 rounded bg-muted/40 border border-border/40 font-mono">
                    <span className="text-[10px] text-muted-foreground flex items-center gap-1">
                      <DollarSign className="size-2.5 text-muted-foreground" /> Cost
                    </span>
                    <strong className="text-foreground text-xs font-semibold">
                      ${sessionData.cost ? sessionData.cost.toFixed(4) : '0.0000'}
                    </strong>
                  </div>
                </div>

                {sessionData.tokens?.reasoning !== undefined && sessionData.tokens.reasoning > 0 && (
                  <div className="text-[11px] font-mono text-purple-600 dark:text-purple-400 bg-purple-500/10 border border-purple-500/20 rounded px-2 py-1 flex items-center justify-between">
                    <span>Reasoning Tokens:</span>
                    <strong>{sessionData.tokens.reasoning.toLocaleString()}</strong>
                  </div>
                )}
              </div>
            )}

            {/* Skills & MCPs */}
            {(task.skills?.length > 0 || task.mcp_servers?.length > 0) && (
              <div className="space-y-2 pt-2">
                <h5 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                  Configured Skills &amp; Integrations
                </h5>
                <div className="flex flex-wrap gap-2">
                  {task.skills?.map((skill) => (
                    <Badge key={skill} variant="outline" className="text-xs font-mono">
                      skill:{skill}
                    </Badge>
                  ))}
                  {task.mcp_servers?.map((mcp) => (
                    <Badge key={mcp} variant="secondary" className="text-xs font-mono">
                      mcp:{mcp}
                    </Badge>
                  ))}
                </div>
              </div>
            )}

            {/* Delegated Subtasks */}
            <div className="space-y-2 pt-2 border-t border-border/60">
              <div className="flex items-center justify-between">
                <h5 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider flex items-center gap-1.5">
                  <GitBranch className="size-3.5 text-primary" />
                  Delegated Subtasks
                </h5>
                <span className="text-xs font-mono text-muted-foreground">
                  {childTasks.length} {childTasks.length === 1 ? 'task' : 'tasks'}
                </span>
              </div>

              {childTasks.length === 0 ? (
                <div className="p-4 rounded-lg border border-dashed border-border/70 bg-muted/15 text-center text-xs text-muted-foreground">
                  No subtasks delegated by this task.
                </div>
              ) : (
                <div className="space-y-1.5">
                  {childTasks.map((child) => (
                    <button
                      key={child.id}
                      type="button"
                      onClick={() => selectTask(child.id)}
                      className="w-full flex items-center justify-between gap-3 p-2.5 rounded-lg border border-border bg-card hover:bg-muted/40 hover:border-primary/40 transition-colors text-left group cursor-pointer"
                    >
                      <div className="flex items-center gap-2 min-w-0">
                        <CornerDownRight className="size-3.5 text-muted-foreground group-hover:text-primary transition-colors shrink-0" />
                        <span className="text-xs font-mono text-muted-foreground shrink-0">
                          #{child.id.slice(0, 8)}
                        </span>
                        <span className="text-xs font-medium text-foreground truncate group-hover:text-primary transition-colors">
                          {child.title}
                        </span>
                      </div>
                      <div className="flex items-center gap-2 shrink-0">
                        {getStatusBadge(child.status)}
                        <ChevronRight className="size-3.5 text-muted-foreground group-hover:text-foreground transition-colors" />
                      </div>
                    </button>
                  ))}
                </div>
              )}
            </div>
          </TabsContent>

          {/* Agent Session Tab */}
          <TabsContent value="session" className="flex-1 mt-4 overflow-hidden h-full">
            <div className="h-full w-full min-h-[380px]">
              <AgentSessionView
                taskId={task.id}
                defaultSessionId={task.session_id}
                isActive={activeTab === 'session'}
              />
            </div>
          </TabsContent>

          {/* Terminal Tab */}
          <TabsContent value="terminal" className="flex-1 mt-4 overflow-hidden h-full">
            <div className="h-full w-full min-h-[380px]">
              <Suspense fallback={<p className="p-4 text-xs text-muted-foreground">Loading terminal…</p>}>
                <TerminalView taskId={task.id} />
              </Suspense>
            </div>
          </TabsContent>

          {/* Diff Tab */}
          <TabsContent value="diff" className="flex-1 mt-4 overflow-hidden h-full">
            <div className="h-full w-full min-h-[380px]">
              <DiffTab />
            </div>
          </TabsContent>

          {/* File Explorer Tab */}
          <TabsContent value="files" className="flex-1 mt-4 overflow-hidden h-full">
            <FileExplorer taskId={task.id} active={activeTab === 'files'} />
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
    </TaskInspectorProvider>
  );
};
