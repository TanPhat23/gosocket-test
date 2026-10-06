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
import { TerminalView } from './TerminalView';
import { DiffViewer } from './DiffViewer';
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
} from 'lucide-react';

export const TaskInspectorModal: React.FC = () => {
  const task = useSelectedTask();
  const selectTask = useTaskStore((state) => state.selectTask);
  const [activeTab, setActiveTab] = useState<string>('overview');

  const isOpen = !!task;

  const handleOpenChange = (open: boolean) => {
    if (!open) {
      selectTask(null);
    }
  };

  if (!task) return null;

  const formatDate = (isoString: string) => {
    try {
      return new Date(isoString).toLocaleString();
    } catch {
      return isoString;
    }
  };

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-4xl max-h-[90vh] h-[85vh] flex flex-col p-6">
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
              <Badge variant="outline" className="font-mono text-xs capitalize">
                {task.status}
              </Badge>
              <Badge variant="secondary" className="font-mono text-xs">
                P{task.priority}
              </Badge>
            </div>
          </div>
        </DialogHeader>

        <Tabs
          value={activeTab}
          onValueChange={(val) => setActiveTab(val as string)}
          className="flex-1 flex flex-col mt-3 overflow-hidden"
        >
          <TabsList className="grid grid-cols-3 w-full max-w-md">
            <TabsTrigger value="overview" className="text-xs flex items-center gap-1.5">
              <Info className="size-3.5" />
              Overview
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
                onApprove={() => {
                  alert(`Approved changes for task ${task.id}. Creating PR.`);
                }}
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
