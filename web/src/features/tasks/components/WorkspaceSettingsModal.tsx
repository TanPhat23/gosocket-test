import React, { useState } from 'react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogTrigger,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  useWorkspaceSettingsStore,
} from '../store/useWorkspaceSettingsStore';
import {
  useProjectsList,
  useActiveProjectId,
  useTeamStore,
} from '@/features/teams/store/useTeamStore';
import { SlidersHorizontal, Cpu, FolderGit2, Check, Shield, GitBranch } from 'lucide-react';

export interface WorkspaceSettingsModalProps {
  trigger?: React.ReactNode;
}

export const WorkspaceSettingsModal: React.FC<WorkspaceSettingsModalProps> = ({ trigger }) => {
  const [open, setOpen] = useState(false);
  const defaultModel = useWorkspaceSettingsStore((s) => s.defaultModel);
  const setDefaultModel = useWorkspaceSettingsStore((s) => s.setDefaultModel);
  const defaultPriority = useWorkspaceSettingsStore((s) => s.defaultPriority);
  const setDefaultPriority = useWorkspaceSettingsStore((s) => s.setDefaultPriority);
  const defaultRepoId = useWorkspaceSettingsStore((s) => s.defaultRepoId);
  const setDefaultRepoId = useWorkspaceSettingsStore((s) => s.setDefaultRepoId);
  const connectedRepos = useWorkspaceSettingsStore((s) => s.connectedRepos);
  const availableModels = useWorkspaceSettingsStore((s) => s.availableModels);

  const projects = useProjectsList();
  const activeProjectId = useActiveProjectId();
  const setActiveProjectId = useTeamStore((s) => s.setActiveProjectId);

  const priorities = [
    { value: 25, label: 'Low (P25)', desc: 'Background & non-critical refactors' },
    { value: 50, label: 'Normal (P50)', desc: 'Standard task execution priority' },
    { value: 80, label: 'High (P80)', desc: 'Urgent fixes and prioritized work' },
  ];

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger
        render={
          trigger ? (
            (trigger as React.ReactElement)
          ) : (
            <Button
              variant="outline"
              size="sm"
              className="h-8 gap-1.5 px-2.5 text-xs text-muted-foreground hover:text-foreground"
              title="Configure workspace defaults"
              data-testid="workspace-settings-trigger"
            >
              <SlidersHorizontal className="size-3.5" />
              <span className="hidden sm:inline">Settings</span>
            </Button>
          )
        }
      />

      <DialogContent className="sm:max-w-md max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <div className="flex items-center gap-2">
            <div className="size-7 rounded-lg bg-primary/10 flex items-center justify-center text-primary">
              <SlidersHorizontal className="size-4" />
            </div>
            <div>
              <DialogTitle className="text-sm font-semibold">Workspace Defaults</DialogTitle>
              <DialogDescription className="text-xs text-muted-foreground">
                Set default model and project so you never have to re-select them repeatedly.
              </DialogDescription>
            </div>
          </div>
        </DialogHeader>

        <div className="space-y-4 pt-2 text-xs">
          {/* Active / Default Project */}
          <div className="space-y-1.5">
            <label className="font-semibold text-foreground flex items-center gap-1.5">
              <FolderGit2 className="size-3.5 text-primary" />
              Default Project
            </label>
            <p className="text-[11px] text-muted-foreground">
              New tasks will automatically target this project by default.
            </p>
            <Select
              value={activeProjectId || 'all'}
              onValueChange={(val) => {
                if (typeof val === 'string') {
                  setActiveProjectId(val === 'all' ? null : val);
                }
              }}
            >
              <SelectTrigger size="sm" className="w-full bg-card text-xs" aria-label="Default Project">
                <SelectValue placeholder="All Projects" />
              </SelectTrigger>
              <SelectContent align="start">
                <SelectItem value="all">-- All Projects (No Specific Project) --</SelectItem>
                {projects.map((p) => (
                  <SelectItem key={p.id} value={p.id}>
                    {p.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Default Target Repository */}
          {connectedRepos.length > 0 && (
            <div className="space-y-1.5">
              <label className="font-semibold text-foreground flex items-center gap-1.5">
                <GitBranch className="size-3.5 text-primary" />
                Default Target Repository
              </label>
              <p className="text-[11px] text-muted-foreground">
                Pre-selected when creating tasks so you never have to pick it repeatedly.
              </p>
              <Select
                value={defaultRepoId || 'auto'}
                onValueChange={(val) => {
                  if (typeof val === 'string') {
                    setDefaultRepoId(val === 'auto' ? '' : val);
                  }
                }}
              >
                <SelectTrigger size="sm" className="w-full bg-card text-xs font-mono" aria-label="Default Repository">
                  <SelectValue placeholder="Auto (First connected repo)" />
                </SelectTrigger>
                <SelectContent align="start">
                  <SelectItem value="auto">-- Auto (First connected repo) --</SelectItem>
                  {connectedRepos.map((r) => (
                    <SelectItem key={r.id} value={r.id}>
                      {r.external_id} ({String(r.config?.base_branch || 'main')})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          {/* Default Autonomous Model */}
          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <label className="font-semibold text-foreground flex items-center gap-1.5">
                <Cpu className="size-3.5 text-primary" />
                Default AI Model
              </label>
              <Badge variant="outline" className="text-[10px] font-mono">
                {availableModels.length} models
              </Badge>
            </div>
            <p className="text-[11px] text-muted-foreground">
              Pre-selected model applied immediately when dispatching tasks.
            </p>

            <div className="grid grid-cols-1 gap-1.5 max-h-48 overflow-y-auto p-1 rounded-lg border border-border/60 bg-muted/20">
              {availableModels.map((m) => {
                const isSelected = defaultModel === m.id;
                return (
                  <button
                    key={m.id}
                    type="button"
                    onClick={() => setDefaultModel(m.id)}
                    className={`text-left p-2 rounded-lg border text-xs transition-colors flex items-center justify-between gap-2 cursor-pointer ${
                      isSelected
                        ? 'border-primary bg-primary/10 text-foreground font-medium shadow-xs'
                        : 'border-border/40 bg-background hover:bg-muted/50 text-muted-foreground'
                    }`}
                  >
                    <div className="min-w-0 flex-1 truncate">
                      <div className="font-mono text-xs truncate flex items-center gap-1.5 text-foreground">
                        {m.name}
                        {m.active && (
                          <span className="size-1.5 rounded-full bg-emerald-500 shrink-0" title="Active provider" />
                        )}
                      </div>
                      <div className="text-[10px] text-muted-foreground truncate">{m.id}</div>
                    </div>
                    {isSelected && <Check className="size-3.5 text-primary shrink-0" />}
                  </button>
                );
              })}
            </div>
          </div>

          {/* Default Priority */}
          <div className="space-y-1.5">
            <label className="font-semibold text-foreground flex items-center gap-1.5">
              <Shield className="size-3.5 text-primary" />
              Default Task Priority
            </label>
            <div className="grid grid-cols-3 gap-2">
              {priorities.map((p) => {
                const isSelected = defaultPriority === p.value;
                return (
                  <button
                    key={p.value}
                    type="button"
                    onClick={() => setDefaultPriority(p.value)}
                    className={`p-2 rounded-lg border text-center transition-colors cursor-pointer ${
                      isSelected
                        ? 'border-primary bg-primary/10 text-foreground font-medium shadow-xs'
                        : 'border-border/50 bg-background hover:bg-muted/50 text-muted-foreground'
                    }`}
                  >
                    <div className="text-xs font-semibold">{p.label}</div>
                    <div className="text-[10px] text-muted-foreground/70 mt-0.5 truncate">{p.desc}</div>
                  </button>
                );
              })}
            </div>
          </div>

          <div className="pt-2 flex justify-end">
            <Button size="sm" onClick={() => setOpen(false)} className="h-8 text-xs font-medium">
              Done
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
};
