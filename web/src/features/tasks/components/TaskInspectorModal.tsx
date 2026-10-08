import React, { useState } from 'react';
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
import { TerminalView } from './TerminalView';
import { DiffViewer } from './DiffViewer';
import { AgentSessionView } from './AgentSessionView';
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
} from 'lucide-react';
import { taskApi } from '../api/taskApi';
import type { TaskStatus } from '../schemas/taskSchemas';

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

export const TaskInspectorModal: React.FC = () => {
  const task = useSelectedTask();
  const selectTask = useTaskStore((state) => state.selectTask);
  const updateTask = useTaskStore((state) => state.updateTask);
  const tasks = useTaskStore((state) => state.tasks);
  const [activeTab, setActiveTab] = useState<string>('overview');

  const [isCreatingPR, setIsCreatingPR] = useState(false);
  const [prError, setPrError] = useState<string | null>(null);
  const [prToast, setPrToast] = useState<{ message: string; url?: string } | null>(null);
  const [copiedSessionId, setCopiedSessionId] = useState(false);
  const [taskDiff, setTaskDiff] = useState<string>('');
  const [loadingDiff, setLoadingDiff] = useState(false);

  React.useEffect(() => {
    if (!task) {
      setTaskDiff('');
      return;
    }
    let isMounted = true;
    setLoadingDiff(true);
    taskApi
      .getTaskDiff(task.id)
      .then((d) => {
        if (isMounted) setTaskDiff(d || '');
      })
      .catch(() => {
        if (isMounted) setTaskDiff('');
      })
      .finally(() => {
        if (isMounted) setLoadingDiff(false);
      });

    return () => {
      isMounted = false;
    };
  }, [task?.id, activeTab]);

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
      selectTask(null);
    }
  };

  if (!task) return null;

  const prLink = task ? task.pull_request_url || task.pr_url : null;
  const isCompleted = task ? ['done', 'review'].includes(task.status) : false;

  const handleCreatePR = async () => {
    if (!task) return;
    setIsCreatingPR(true);
    setPrError(null);
    try {
      const res = await taskApi.createTaskPR(task.id);
      const updatedPrUrl = res.pull_request_url;
      const updated: typeof task = {
        ...task,
        pull_request_url: updatedPrUrl,
        pr_url: updatedPrUrl,
        updated_at: new Date().toISOString(),
      };
      updateTask(updated);
      setPrToast({
        message: 'Pull request created successfully!',
        url: updatedPrUrl,
      });
      setTimeout(() => {
        setPrToast(null);
      }, 5000);
    } catch (err) {
      setPrError(err instanceof Error ? err.message : 'Failed to create pull request');
    } finally {
      setIsCreatingPR(false);
    }
  };

  const formatDate = (isoString: string) => {
    try {
      return new Date(isoString).toLocaleString();
    } catch {
      return isoString;
    }
  };

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-5xl lg:max-w-6xl xl:max-w-7xl max-h-[92vh] h-[90vh] flex flex-col p-6 w-full">
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

              {/* Pull Request Link */}
              {prLink && (
                <a
                  href={prLink}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 text-xs font-mono text-emerald-600 dark:text-emerald-400 bg-emerald-500/15 hover:bg-emerald-500/25 border border-emerald-500/30 rounded px-2 py-0.5 font-medium transition-colors"
                  title="View Pull Request"
                  data-testid="inspector-pr-link"
                >
                  <GitPullRequest className="size-3" />
                  <span>Pull Request</span>
                  <ExternalLink className="size-2.5" />
                </a>
              )}

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

        {/* Actions Bar for Tasks */}
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

            {prLink && (
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
            )}
          </div>
        </div>

        <Tabs
          value={activeTab}
          onValueChange={(val) => setActiveTab(val as string)}
          className="flex-1 flex flex-col mt-3 overflow-hidden"
        >
          <TabsList className="grid grid-cols-4 w-full max-w-xl">
            <TabsTrigger value="overview" className="text-xs flex items-center gap-1.5">
              <Info className="size-3.5" />
              Overview
            </TabsTrigger>
            <TabsTrigger value="session" className="text-xs flex items-center gap-1.5" data-testid="tab-agent-session">
              <Bot className="size-3.5" />
              Agent Session
            </TabsTrigger>
            <TabsTrigger value="terminal" className="text-xs flex items-center gap-1.5">
              <TerminalIcon className="size-3.5" />
              Terminal Logs
            </TabsTrigger>
            <TabsTrigger value="diff" className="text-xs flex items-center gap-1.5">
              <GitCompare className="size-3.5" />
              Git Diff Review
            </TabsTrigger>
          </TabsList>

          {/* Overview Tab */}
          <TabsContent value="overview" className="flex-1 overflow-y-auto mt-4 space-y-4 pr-1">
            {/* Prompt block */}
            <div className="space-y-1.5">
              <h4 className="text-xs font-semibold text-foreground uppercase tracking-wider">
                Prompt &amp; Specification
              </h4>
              <div className="p-3 rounded-lg border border-border bg-muted/30 font-mono text-xs whitespace-pre-wrap leading-relaxed text-foreground">
                {task.prompt}
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
              <AgentSessionView taskId={task.id} defaultSessionId={task.session_id} />
            </div>
          </TabsContent>

          {/* Terminal Tab */}
          <TabsContent value="terminal" className="flex-1 mt-4 overflow-hidden h-full">
            <div className="h-full w-full min-h-[380px]">
              <TerminalView taskId={task.id} />
            </div>
          </TabsContent>

          {/* Diff Tab */}
          <TabsContent value="diff" className="flex-1 mt-4 overflow-hidden h-full">
            <div className="h-full w-full min-h-[380px]">
              <DiffViewer
                diff={taskDiff}
                loading={loadingDiff}
                onApprove={handleCreatePR}
                onReject={() => {
                  alert(`Rejected changes for task ${task.id}. Prompting agent for fixes.`);
                }}
              />
            </div>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
};
