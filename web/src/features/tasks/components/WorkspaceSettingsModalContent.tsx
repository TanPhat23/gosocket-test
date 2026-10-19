import React from 'react';
import { Cpu, FolderGit2, GitBranch, GitCommitHorizontal, Shield, SlidersHorizontal } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { GitIdentitySetting } from '@/features/auth/components/GitIdentitySetting';
import { useActiveProjectId, useProjectsList, useTeamStore } from '@/features/teams/store/useTeamStore';
import { useWorkspaceSettingsStore } from '../store/useWorkspaceSettingsStore';
import { WorkspaceModelPicker } from './WorkspaceModelPicker';
import { WorkspacePriorityPicker } from './WorkspacePriorityPicker';

interface WorkspaceSettingsModalContentProps {
  onOpenChange: (open: boolean) => void;
}

interface SettingProps {
  icon: React.ReactNode;
  title: string;
  hint: string;
  aside?: React.ReactNode;
  children: React.ReactNode;
}

// One setting: what it is, the control, and a line on what it does.
function Setting({ icon, title, hint, aside, children }: SettingProps) {
  return (
    <section className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <h3 className="flex items-center gap-1.5 text-xs font-semibold text-foreground">
          <span className="text-primary">{icon}</span>
          {title}
        </h3>
        {aside}
      </div>
      {children}
      <p className="text-[11px] text-muted-foreground">{hint}</p>
    </section>
  );
}

export const WorkspaceSettingsModalContent: React.FC<WorkspaceSettingsModalContentProps> = ({ onOpenChange }) => {
  const defaultRepoId = useWorkspaceSettingsStore((s) => s.defaultRepoId);
  const setDefaultRepoId = useWorkspaceSettingsStore((s) => s.setDefaultRepoId);
  const connectedRepos = useWorkspaceSettingsStore((s) => s.connectedRepos);
  const modelCount = useWorkspaceSettingsStore((s) => s.availableModels.length);

  const projects = useProjectsList();
  const activeProjectId = useActiveProjectId();
  const setActiveProjectId = useTeamStore((s) => s.setActiveProjectId);

  // Base UI resolves each trigger label from the root's items, so names show instead of ids.
  const projectItems = [
    { value: 'all', label: 'All projects' },
    ...projects.map((p) => ({ value: p.id, label: p.name })),
  ];
  const repoItems = [
    { value: 'auto', label: 'Auto (first connected repo)' },
    ...connectedRepos.map((r) => ({ value: r.id, label: `${r.external_id} (${String(r.config?.base_branch || 'main')})` })),
  ];

  return (
    <DialogContent className="sm:max-w-xl max-h-[88vh] gap-0 overflow-y-auto p-0">
      <DialogHeader className="flex-row items-start gap-3 border-b border-border/60 p-5 pr-12">
        <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
          <SlidersHorizontal className="size-4" />
        </div>
        <div className="space-y-0.5">
          <DialogTitle className="text-base font-semibold">Workspace Defaults</DialogTitle>
          <DialogDescription className="text-xs">
            Pick these once and every new task starts with them.
          </DialogDescription>
        </div>
      </DialogHeader>

      <div className="space-y-6 p-5">
        <div className={`grid gap-4 ${connectedRepos.length > 0 ? 'sm:grid-cols-2' : ''}`}>
          <Setting icon={<FolderGit2 className="size-3.5" />} title="Default Project" hint="New tasks target this project.">
            <Select
              items={projectItems}
              value={activeProjectId || 'all'}
              onValueChange={(val) => {
                if (typeof val === 'string') setActiveProjectId(val === 'all' ? null : val);
              }}
            >
              <SelectTrigger className="w-full bg-card text-xs" aria-label="Default Project">
                <SelectValue placeholder="All projects" />
              </SelectTrigger>
              <SelectContent align="start">
                <SelectGroup>
                  {projectItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </Setting>

          {connectedRepos.length > 0 && (
            <Setting icon={<GitBranch className="size-3.5" />} title="Default Repository" hint="Pre-selected when you create a task.">
              <Select
                items={repoItems}
                value={defaultRepoId || 'auto'}
                onValueChange={(val) => {
                  if (typeof val === 'string') setDefaultRepoId(val === 'auto' ? '' : val);
                }}
              >
                <SelectTrigger className="w-full bg-card font-mono text-xs" aria-label="Default Repository">
                  <SelectValue placeholder="Auto (first connected repo)" />
                </SelectTrigger>
                <SelectContent align="start">
                  <SelectGroup>
                    {repoItems.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Setting>
          )}
        </div>

        <Setting
          icon={<Cpu className="size-3.5" />}
          title="Default AI Model"
          hint="Applied when you dispatch a task."
          aside={
            <Badge variant="outline" className="font-mono text-[10px]">
              {modelCount} models
            </Badge>
          }
        >
          <WorkspaceModelPicker />
        </Setting>

        <Setting icon={<Shield className="size-3.5" />} title="Default Task Priority" hint="Higher priority tasks are picked up first.">
          <WorkspacePriorityPicker />
        </Setting>

        <Setting
          icon={<GitCommitHorizontal className="size-3.5" />}
          title="Git Identity"
          hint="Commits on the branches of tasks you create are authored as this name and email. Leave both empty to use the default."
        >
          <GitIdentitySetting />
        </Setting>
      </div>

      <DialogFooter className="m-0 rounded-b-xl">
        <Button size="sm" onClick={() => onOpenChange(false)} className="h-8 px-5 text-xs font-medium">
          Done
        </Button>
      </DialogFooter>
    </DialogContent>
  );
};
