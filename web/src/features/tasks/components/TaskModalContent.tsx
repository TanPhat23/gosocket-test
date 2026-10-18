import React, { useState, useMemo } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useTaskStore } from '../store/useTaskStore';
import { useProjectsList, useActiveProjectId, useActiveTeamId } from '@/features/teams/store/useTeamStore';
import { CreateTaskRequestSchema } from '../schemas/taskSchemas';
import {
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Check, Sparkles, Server, Wrench, AlertCircle, Cpu, GitBranch, FolderGit2, RefreshCw } from 'lucide-react';
import { integrationApi } from '@/features/integrations/api/integrationApi';
import { projectIntegrationsQueryOptions, gitHubBranchesQueryOptions } from '@/features/integrations/queries';
import { modelsQueryOptions } from '../queries';
import {
  useWorkspaceSettingsStore,
  type ModelOption,
  DEFAULT_OPENCODE_MODELS,
} from '../store/useWorkspaceSettingsStore';
import { cn } from '@/lib/utils';


const AVAILABLE_SKILLS = [
  'frontend-ui-engineering',
  'systematic-debugging',
  'code-review-and-quality',
  'browser-testing-with-devtools',
  'test-driven-development',
];

const AVAILABLE_MCPS = [
  'github',
  'context7',
  'playwright',
  'postgres',
  'docker',
];

interface TaskModalContentProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export const TaskModalContent: React.FC<TaskModalContentProps> = ({ open, onOpenChange }) => {
  const queryClient = useQueryClient();
  const createTask = useTaskStore((state) => state.createTask);
  const projects = useProjectsList();
  const activeProjectId = useActiveProjectId();
  const activeTeamId = useActiveTeamId();

  const defaultModel = useWorkspaceSettingsStore((state) => state.defaultModel);
  const defaultPriority = useWorkspaceSettingsStore((state) => state.defaultPriority);
  const selectedRepoId = useWorkspaceSettingsStore((state) => state.selectedRepoId);
  const defaultRepoId = useWorkspaceSettingsStore((state) => state.defaultRepoId);
  const workspaceModels = useWorkspaceSettingsStore((state) => state.availableModels);

  // Queries for models
  const { data: fetchedModels } = useQuery(modelsQueryOptions());
  const availableModels: ModelOption[] = useMemo(() => {
    if (fetchedModels && fetchedModels.length > 0) return fetchedModels;
    if (workspaceModels && workspaceModels.length > 0) return workspaceModels;
    return DEFAULT_OPENCODE_MODELS;
  }, [fetchedModels, workspaceModels]);

  const [selectedProjectId, setSelectedProjectId] = useState<string>(activeProjectId || '');
  const [title, setTitle] = useState('');
  const [prompt, setPrompt] = useState('');
  const [priority, setPriority] = useState<number>(defaultPriority || 50);
  const [selectedModel, setSelectedModel] = useState<string>(defaultModel || 'google/gemini-3.8-flash');
  const [selectedSkills, setSelectedSkills] = useState<string[]>([]);
  const [selectedMcps, setSelectedMcps] = useState<string[]>([]);
  const [errors, setErrors] = useState<Record<string, string>>({});

  // Sync project ID when projects load
  React.useEffect(() => {
    if (activeProjectId) {
      setSelectedProjectId(activeProjectId);
    } else if (projects.length > 0 && !selectedProjectId) {
      setSelectedProjectId(projects[0].id);
    }
  }, [activeProjectId, projects, selectedProjectId]);

  const effectiveProjectId = selectedProjectId || activeProjectId || (projects.length > 0 ? projects[0].id : '');

  // Query project integrations
  const { data: rawIntegrations } = useQuery({
    ...projectIntegrationsQueryOptions(effectiveProjectId),
    enabled: Boolean(effectiveProjectId),
  });

  const projectIntegrations = useMemo(() => {
    return (rawIntegrations || []).filter((i) => i.provider === 'github' && i.enabled);
  }, [rawIntegrations]);

  const [selectedIntegrationId, setSelectedIntegrationId] = useState<string>('');
  const [selectedBranch, setSelectedBranch] = useState<string>('');

  // Sync selected integration based on preferences or available integrations
  React.useEffect(() => {
    if (projectIntegrations.length > 0) {
      const preferredRepo = selectedRepoId !== 'all' ? selectedRepoId : defaultRepoId;
      const matchPreferred = projectIntegrations.find((g) => g.id === preferredRepo);
      if (matchPreferred) {
        setSelectedIntegrationId(matchPreferred.id);
      } else {
        setSelectedIntegrationId((prev) => {
          const exists = projectIntegrations.some((g) => g.id === prev);
          return exists ? prev : projectIntegrations[0].id;
        });
      }
    } else {
      setSelectedIntegrationId('');
    }
  }, [projectIntegrations, selectedRepoId, defaultRepoId]);

  const selectedInteg = useMemo(
    () => projectIntegrations.find((i) => i.id === selectedIntegrationId),
    [projectIntegrations, selectedIntegrationId]
  );

  // Query branches for selected integration
  const shouldFetchBranches = Boolean(
    activeTeamId && selectedInteg?.credential_id && selectedInteg?.external_id
  );

  const { data: fetchedBranches, isLoading: loadingBranches } = useQuery({
    ...gitHubBranchesQueryOptions(
      activeTeamId || '',
      selectedInteg?.credential_id || '',
      selectedInteg?.external_id || ''
    ),
    // TaskModal is mounted on every workspace page, so the GitHub branch lookup waits until the dialog opens.
    enabled: open && shouldFetchBranches,
  });

  const defaultBaseBranch = String(selectedInteg?.config?.base_branch || 'main');
  const repoBranches = useMemo(() => {
    if (fetchedBranches && fetchedBranches.length > 0) {
      return fetchedBranches;
    }
    return selectedInteg ? [defaultBaseBranch] : [];
  }, [fetchedBranches, selectedInteg, defaultBaseBranch]);

  // Keep selected branch updated
  React.useEffect(() => {
    if (!selectedInteg) {
      setSelectedBranch('');
      return;
    }
    if (fetchedBranches && fetchedBranches.length > 0) {
      if (fetchedBranches.includes(defaultBaseBranch)) {
        setSelectedBranch(defaultBaseBranch);
      } else {
        setSelectedBranch(fetchedBranches[0]);
      }
    } else {
      setSelectedBranch(defaultBaseBranch);
    }
  }, [selectedInteg, fetchedBranches, defaultBaseBranch]);

  // Sync with workspace settings defaults when opening
  React.useEffect(() => {
    if (open) {
      if (defaultModel) setSelectedModel(defaultModel);
      if (defaultPriority) setPriority(defaultPriority);
      if (activeProjectId) setSelectedProjectId(activeProjectId);
    }
  }, [open, defaultModel, defaultPriority, activeProjectId]);

  const createTaskMutation = useMutation({
    mutationFn: async (taskData: Parameters<typeof createTask>[0]) => {
      // If a branch was customized, ensure integration config base_branch reflects the selected branch
      if (effectiveProjectId && selectedIntegrationId && selectedBranch) {
        const currentIntegBranch = String(selectedInteg?.config?.base_branch || 'main');
        if (selectedBranch !== currentIntegBranch) {
          try {
            await integrationApi.updateProjectIntegration(effectiveProjectId, selectedIntegrationId, {
              base_branch: selectedBranch,
            });
          } catch (err) {
            console.warn('Failed to update integration base_branch before dispatch:', err);
          }
        }
      }

      return await createTask(taskData);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] });
      resetForm();
      onOpenChange(false);
    },
    onError: (err) => {
      setErrors({ form: err instanceof Error ? err.message : 'Failed to create task' });
    },
  });

  const toggleSkill = (skill: string) => {
    setSelectedSkills((prev) =>
      prev.includes(skill) ? prev.filter((s) => s !== skill) : [...prev, skill]
    );
  };

  const toggleMcp = (mcp: string) => {
    setSelectedMcps((prev) =>
      prev.includes(mcp) ? prev.filter((m) => m !== mcp) : [...prev, mcp]
    );
  };

  const resetForm = () => {
    setTitle('');
    setPrompt('');
    setPriority(defaultPriority || 50);
    setSelectedModel(defaultModel || 'google/gemini-3.8-flash');
    setSelectedSkills([]);
    setSelectedMcps([]);
    setSelectedIntegrationId('');
    setSelectedBranch('');
    setErrors({});
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrors({});

    const rawData = {
      project_id: effectiveProjectId.trim(),
      integration_id: selectedIntegrationId ? selectedIntegrationId.trim() : undefined,
      title: title.trim(),
      prompt: prompt.trim(),
      assignee_id: 'agent-sandbox-1',
      assignee_type: 'agent' as const,
      priority: Number(priority),
      model: selectedModel.trim() || undefined,
      skills: selectedSkills,
      mcp_servers: selectedMcps,
    };

    const validation = CreateTaskRequestSchema.safeParse(rawData);
    if (!validation.success) {
      const fieldErrors: Record<string, string> = {};
      for (const issue of validation.error.issues) {
        if (issue.path.length > 0) {
          fieldErrors[issue.path[0].toString()] = issue.message;
        }
      }
      setErrors(fieldErrors);
      return;
    }

    createTaskMutation.mutate(validation.data);
  };

  const isSubmitting = createTaskMutation.isPending;

  return (

    <DialogContent className="sm:max-w-xl max-h-[90vh] overflow-y-auto">
      <DialogHeader>
        <DialogTitle className="text-base font-semibold flex items-center gap-2">
          <Sparkles className="size-4 text-primary" />
          Create Autonomous Agent Task
        </DialogTitle>
      </DialogHeader>

      <form onSubmit={handleSubmit} className="space-y-4 pt-1">
        {errors.form && (
          <div className="p-3 text-xs rounded-lg bg-destructive/15 text-destructive flex items-center gap-2">
            <AlertCircle className="size-4 shrink-0" />
            <span>{errors.form}</span>
          </div>
        )}

        {/* Project selection */}
        <div className="space-y-1.5">
          <label className="text-xs font-semibold text-foreground flex items-center gap-1.5">
            <FolderGit2 className="size-3.5 text-primary" />
            Project
          </label>
          {projects.length > 0 ? (
            <Select
              value={effectiveProjectId || 'none'}
              onValueChange={(val) => {
                if (typeof val === 'string') {
                  setSelectedProjectId(val === 'none' ? '' : val);
                }
              }}
            >
              <SelectTrigger size="sm" className="w-full bg-card text-xs" aria-label="Project">
                <SelectValue placeholder="Select project..." />
              </SelectTrigger>
              <SelectContent align="start">
                <SelectItem value="none">-- No Project (Empty) --</SelectItem>
                {projects.map((proj) => (
                  <SelectItem key={proj.id} value={proj.id}>
                    {proj.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <Input
              id="task-project-input"
              aria-label="Project"
              value={selectedProjectId}
              onChange={(e) => setSelectedProjectId(e.target.value)}
              placeholder="Project UUID (leave empty to test)"
              className="text-xs font-mono"
            />
          )}
          {errors.project_id && (
            <p className="text-[11px] text-destructive">{errors.project_id}</p>
          )}
        </div>

        {/* Target Repository Selection (when project has connected GitHub repos) */}
        {projectIntegrations.length > 0 && (
          <div className="space-y-3 p-3 rounded-lg border border-border/70 bg-muted/20">
            <div className="flex items-center justify-between">
              <label className="text-xs font-semibold text-foreground flex items-center gap-1.5">
                <GitBranch className="size-3.5 text-primary" />
                Target Repository &amp; Base Branch
              </label>
              <Badge variant="outline" className="text-[9px] font-mono">
                {projectIntegrations.length} connected
              </Badge>
            </div>

            {projectIntegrations.length === 1 ? (
              <div className="flex items-center gap-2 text-xs font-mono text-muted-foreground bg-background px-2.5 py-1.5 rounded border border-border/50">
                <span className="size-1.5 rounded-full bg-emerald-500 shrink-0" />
                <span className="text-foreground font-medium">{projectIntegrations[0].external_id}</span>
              </div>
            ) : (
              <div className="space-y-1.5">
                <p className="text-[11px] text-muted-foreground">
                  Select which repository the agent should clone, work on, and create branches in:
                </p>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-2" role="radiogroup" aria-label="Target Repository">
                  {projectIntegrations.map((integ) => {
                    const isSelected = selectedIntegrationId === integ.id;
                    return (
                      <button
                        key={integ.id}
                        type="button"
                        onClick={() => setSelectedIntegrationId(integ.id)}
                        data-testid={`target-repo-${integ.id}`}
                        className={`text-left p-2 rounded-lg border text-xs transition-colors flex items-center justify-between gap-2 cursor-pointer ${
                          isSelected
                            ? 'border-primary bg-primary/10 text-foreground font-medium shadow-xs'
                            : 'border-border/60 bg-background hover:bg-muted/40 text-muted-foreground'
                        }`}
                      >
                        <div className="min-w-0 flex-1 truncate">
                          <div className="font-mono text-xs truncate text-foreground">{integ.external_id}</div>
                          <div className="text-[10px] text-muted-foreground truncate">
                            Configured: {String(integ.config?.base_branch || 'main')}
                          </div>
                        </div>
                        {isSelected && (
                          <span className="size-2 rounded-full bg-primary shrink-0" />
                        )}
                      </button>
                    );
                  })}
                </div>
              </div>
            )}

            {/* Base Branch Selection for the chosen repository */}
            <div className="space-y-1.5 pt-1 border-t border-border/40">
              <div className="flex items-center justify-between">
                <label htmlFor="target-base-branch" className="text-xs font-medium text-foreground flex items-center gap-1.5">
                  <GitBranch className="size-3 text-primary" />
                  <span>Base Branch</span>
                </label>
                {loadingBranches ? (
                  <span className="text-[10px] text-muted-foreground flex items-center gap-1">
                    <RefreshCw className="size-2.5 animate-spin text-primary" />
                    <span>Fetching branches from GitHub...</span>
                  </span>
                ) : repoBranches.length > 0 ? (
                  <span className="text-[10px] text-muted-foreground font-mono">
                    {repoBranches.length} branches found
                  </span>
                ) : null}
              </div>

              {repoBranches.length > 0 ? (
                <Select
                  value={selectedBranch || repoBranches[0]}
                  onValueChange={(val) => {
                    if (typeof val === 'string') {
                      setSelectedBranch(val);
                    }
                  }}
                >
                  <SelectTrigger
                    id="target-base-branch"
                    size="sm"
                    className="w-full bg-background text-xs font-mono"
                    aria-label="Base Branch"
                    data-testid="target-branch-select"
                  >
                    <SelectValue placeholder="Select branch..." />
                  </SelectTrigger>
                  <SelectContent align="start">
                    {repoBranches.map((branch) => (
                      <SelectItem key={branch} value={branch} className="font-mono text-xs">
                        {branch}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : (
                <Input
                  id="target-base-branch"
                  value={selectedBranch}
                  onChange={(e) => setSelectedBranch(e.target.value)}
                  placeholder="main"
                  className="text-xs font-mono h-8 bg-background"
                  data-testid="target-branch-input"
                />
              )}
            </div>
          </div>
        )}

        {/* Title */}
        <div className="space-y-1.5">
          <label htmlFor="task-title" className="text-xs font-semibold text-foreground">
            Task Title
          </label>
          <Input
            id="task-title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="e.g. Implement OIDC token refresh worker in internal/auth"
            className="text-xs"
            autoFocus
          />
          {errors.title && <p className="text-[11px] text-destructive">{errors.title}</p>}
        </div>

        {/* Prompt */}
        <div className="space-y-1.5">
          <label htmlFor="task-prompt" className="text-xs font-semibold text-foreground">
            Prompt &amp; Execution Specification
          </label>
          <Textarea
            id="task-prompt"
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="Detailed instructions, files to modify, constraints, and test requirements..."
            rows={4}
            className="text-xs min-h-[90px] font-sans"
          />
          {errors.prompt && <p className="text-[11px] text-destructive">{errors.prompt}</p>}
        </div>

        {/* Priority & Model */}
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          {/* Priority */}
          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <label className="text-xs font-semibold text-foreground">Priority ({priority})</label>
              <span className="text-[11px] font-mono text-muted-foreground">
                {priority >= 80 ? 'Critical' : priority >= 60 ? 'High' : priority >= 30 ? 'Normal' : 'Low'}
              </span>
            </div>
            <input
              type="range"
              min="1"
              max="100"
              value={priority}
              onChange={(e) => setPriority(Number(e.target.value))}
              className="w-full accent-primary h-2 bg-muted rounded-lg appearance-none cursor-pointer"
            />
          </div>

          {/* Model Engine */}
          {/* Model Selection */}
          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <label className="text-xs font-semibold text-foreground flex items-center gap-1.5">
                <Cpu className="size-3.5 text-primary" />
                Model Engine (OpenCode)
              </label>
            </div>
            <Select
              value={selectedModel}
              onValueChange={(val) => {
                if (typeof val === 'string') {
                  setSelectedModel(val);
                }
              }}
            >
              <SelectTrigger size="sm" className="w-full bg-card text-xs font-mono" aria-label="Model Engine">
                <SelectValue placeholder="Select model..." />
              </SelectTrigger>
              <SelectContent align="start">
                {availableModels.map((model) => (
                  <SelectItem key={model.id} value={model.id}>
                    {model.name} ({model.id})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        {/* Skills (Toggleable badge list) */}
        <div className="space-y-1.5">
          <div className="flex items-center gap-1.5">
            <Wrench className="size-3.5 text-muted-foreground" />
            <label className="text-xs font-semibold text-foreground">Agent Skills</label>
          </div>
          <div className="flex flex-wrap gap-1.5 pt-1">
            {AVAILABLE_SKILLS.map((skill) => {
              const isSelected = selectedSkills.includes(skill);
              return (
                <Badge
                  key={skill}
                  variant={isSelected ? 'default' : 'outline'}
                  onClick={() => toggleSkill(skill)}
                  className={cn(
                    'cursor-pointer select-none text-[11px] transition-all py-0.5 px-2 font-normal',
                    isSelected ? 'shadow-xs' : 'hover:bg-muted opacity-75 hover:opacity-100'
                  )}
                >
                  {isSelected && <Check className="size-3 mr-1" />}
                  {skill}
                </Badge>
              );
            })}
          </div>
        </div>

        {/* MCP Servers (Toggleable badge list) */}
        <div className="space-y-1.5">
          <div className="flex items-center gap-1.5">
            <Server className="size-3.5 text-muted-foreground" />
            <label className="text-xs font-semibold text-foreground">MCP Servers</label>
          </div>
          <div className="flex flex-wrap gap-1.5 pt-1">
            {AVAILABLE_MCPS.map((mcp) => {
              const isSelected = selectedMcps.includes(mcp);
              return (
                <Badge
                  key={mcp}
                  variant={isSelected ? 'default' : 'outline'}
                  onClick={() => toggleMcp(mcp)}
                  className={cn(
                    'cursor-pointer select-none text-[11px] transition-all py-0.5 px-2 font-normal',
                    isSelected ? 'shadow-xs' : 'hover:bg-muted opacity-75 hover:opacity-100'
                  )}
                >
                  {isSelected && <Check className="size-3 mr-1" />}
                  {mcp}
                </Badge>
              );
            })}
          </div>
        </div>

        <DialogFooter className="pt-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              resetForm();
              onOpenChange(false);
            }}
          >
            Cancel
          </Button>
          <Button type="submit" size="sm" disabled={isSubmitting}>
            {isSubmitting ? 'Creating...' : 'Dispatch Task'}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  );
};
