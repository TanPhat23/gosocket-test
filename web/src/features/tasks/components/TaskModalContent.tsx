import React, { useMemo } from 'react';
import { useForm, useStore } from '@tanstack/react-form';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { z } from 'zod';
import { useTaskStore } from '../store/useTaskStore';
import { useProjectsList, useActiveProjectId, useActiveTeamId } from '@/features/teams/store/useTeamStore';
import type { CreateTaskRequest } from '../schemas/taskSchemas';
import {
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { Button } from '@/components/ui/button';
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { AlertCircle, AlertTriangle, ImagePlus, X } from 'lucide-react';
import {
  readSteerImages,
  type SteerImage,
  MAX_STEER_IMAGES,
  STEER_IMAGE_TYPES,
} from '../utils/steerImages';
import { integrationApi } from '@/features/integrations/api/integrationApi';
import type { IntegrationDTO } from '@/features/integrations/schemas/integrationSchemas';
import { projectIntegrationsQueryOptions, gitHubBranchesQueryOptions } from '@/features/integrations/queries';
import { modelsQueryOptions } from '../queries';
import {
  useWorkspaceSettingsStore,
  type ModelOption,
  DEFAULT_OPENCODE_MODELS,
} from '../store/useWorkspaceSettingsStore';

const DEFAULT_MODEL = 'google/gemini-3.8-flash';

// Title and prompt are the only fields the dialog validates on the client; the API checks the rest.
const taskFormSchema = z.object({
  project_id: z.string(),
  integration_id: z.string(),
  base_branch: z.string(),
  model: z.string(),
  title: z.string().trim().min(1, 'Title is required').max(255, 'Title too long'),
  prompt: z.string().trim().min(1, 'Prompt is required'),
});

interface CreateTaskArgs {
  taskData: CreateTaskRequest;
  projectId: string;
  integration?: IntegrationDTO;
  baseBranch: string;
}

function errorText(errors: unknown[]): string | undefined {
  const first = errors[0];
  if (!first) return undefined;
  if (typeof first === 'string') return first;
  return (first as { message?: string }).message;
}

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

  const [images, setImages] = React.useState<SteerImage[]>([]);
  const [imageError, setImageError] = React.useState<string | null>(null);
  const imageInputRef = React.useRef<HTMLInputElement>(null);

  const attachImages = async (files: File[]) => {
    const { images: added, error: attachError } = await readSteerImages(files, images.length);
    if (attachError) setImageError(attachError);
    if (added.length) setImages((prev) => [...prev, ...added]);
  };

  const removeImage = (id: string) => setImages((prev) => prev.filter((img) => img.id !== id));

  const defaultModel = useWorkspaceSettingsStore((state) => state.defaultModel);
  const defaultPriority = useWorkspaceSettingsStore((state) => state.defaultPriority);
  const selectedRepoId = useWorkspaceSettingsStore((state) => state.selectedRepoId);
  const defaultRepoId = useWorkspaceSettingsStore((state) => state.defaultRepoId);
  const workspaceModels = useWorkspaceSettingsStore((state) => state.availableModels);

  // Priority is a workspace setting, not a per-task field in the dialog.
  const priority = defaultPriority || 50;

  const { data: fetchedModels } = useQuery(modelsQueryOptions());
  const availableModels: ModelOption[] = useMemo(() => {
    if (fetchedModels && fetchedModels.length > 0) return fetchedModels;
    if (workspaceModels && workspaceModels.length > 0) return workspaceModels;
    return DEFAULT_OPENCODE_MODELS;
  }, [fetchedModels, workspaceModels]);

  const fallbackProjectId = activeProjectId || projects[0]?.id || '';

  const createTaskMutation = useMutation({
    mutationFn: async ({ taskData, projectId, integration, baseBranch }: CreateTaskArgs) => {
      // A branch picked in the dialog becomes the repository's configured base branch before the task is dispatched.
      if (integration && baseBranch !== String(integration.config?.base_branch || 'main')) {
        try {
          await integrationApi.updateProjectIntegration(projectId, integration.id, { base_branch: baseBranch });
        } catch (err) {
          console.warn('Failed to update integration base_branch before dispatch:', err);
        }
      }
      return await createTask(taskData);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] });
    },
  });

  const form = useForm({
    defaultValues: {
      project_id: '',
      integration_id: '',
      base_branch: '',
      model: defaultModel || DEFAULT_MODEL,
      title: '',
      prompt: '',
    },
    validators: {
      onSubmit: taskFormSchema,
    },
    onSubmit: async ({ value }) => {
      try {
        await createTaskMutation.mutateAsync({
          taskData: {
            project_id: projectId.trim(),
            integration_id: integrationId || undefined,
            title: value.title.trim(),
            prompt: value.prompt.trim(),
            assignee_id: 'agent-sandbox-1',
            assignee_type: 'agent',
            priority: Number(priority),
            model: value.model.trim() || undefined,
            images: images.length ? images.map(({ name, mime, data }) => ({ name, mime, data })) : undefined,
          },
          projectId: projectId.trim(),
          integration: selectedInteg,
          baseBranch,
        });
      } catch {
        // The mutation keeps the error, which the form shows at the top.
        return;
      }
      resetDetails();
      onOpenChange(false);
    },
  });

  const values = useStore(form.store, (state) => state.values);
  const isSubmitting = useStore(form.store, (state) => state.isSubmitting);

  const projectId = values.project_id || fallbackProjectId;

  // Keep the dialog in step with the workspace defaults each time it opens.
  React.useEffect(() => {
    if (!open) return;
    if (defaultModel) form.setFieldValue('model', defaultModel);
    if (activeProjectId) form.setFieldValue('project_id', activeProjectId);
  }, [open, defaultModel, activeProjectId, form]);

  const { data: rawIntegrations } = useQuery({
    ...projectIntegrationsQueryOptions(projectId),
    enabled: Boolean(projectId),
  });

  const projectIntegrations = useMemo(() => {
    return (rawIntegrations || []).filter((i) => i.provider === 'github' && i.enabled);
  }, [rawIntegrations]);

  const preferredRepoId = selectedRepoId !== 'all' ? selectedRepoId : defaultRepoId;
  const selectedInteg =
    projectIntegrations.find((i) => i.id === values.integration_id) ??
    projectIntegrations.find((i) => i.id === preferredRepoId) ??
    projectIntegrations[0];
  const integrationId = selectedInteg?.id ?? '';

  const shouldFetchBranches = Boolean(
    activeTeamId && selectedInteg?.credential_id && selectedInteg?.external_id
  );

  const { data: fetchedBranches } = useQuery({
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

  const preferredBranch =
    fetchedBranches && fetchedBranches.length > 0
      ? fetchedBranches.includes(defaultBaseBranch)
        ? defaultBaseBranch
        : fetchedBranches[0]
      : defaultBaseBranch;
  const baseBranch = repoBranches.includes(values.base_branch) ? values.base_branch : preferredBranch;

  const resetDetails = () => {
    form.reset({ ...form.state.values, title: '', prompt: '' });
    setImages([]);
    setImageError(null);
    createTaskMutation.reset();
  };

  // Base UI resolves a trigger's label from the root's items, so each select gets its label list.
  const projectItems = [
    { value: 'none', label: 'No project' },
    ...projects.map((proj) => ({ value: proj.id, label: proj.name })),
  ];
  const modelItems = availableModels.map((model) => ({ value: model.id, label: model.name }));
  const repoItems = projectIntegrations.map((integ) => ({ value: integ.id, label: integ.external_id }));
  const branchItems = repoBranches.map((branch) => ({ value: branch, label: branch }));

  const submitError = createTaskMutation.isError
    ? createTaskMutation.error instanceof Error
      ? createTaskMutation.error.message
      : 'Failed to create task'
    : null;

  return (
    <DialogContent className="sm:max-w-lg max-h-[90vh] overflow-y-auto">
      <DialogHeader>
        <DialogTitle className="text-base font-semibold">Create task</DialogTitle>
      </DialogHeader>

      <form
        onSubmit={(e) => {
          e.preventDefault();
          e.stopPropagation();
          form.handleSubmit();
        }}
        className="space-y-4 pt-1"
      >
        {submitError && (
          <div className="p-3 text-xs rounded-lg bg-destructive/15 text-destructive flex items-center gap-2">
            <AlertCircle className="size-4 shrink-0" />
            <span>{submitError}</span>
          </div>
        )}

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <form.Field
            name="project_id"
            children={(field) => (
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-foreground">Project</label>
                {projects.length > 0 ? (
                  <Select
                    items={projectItems}
                    value={projectId || 'none'}
                    onValueChange={(val) => {
                      if (typeof val === 'string') field.handleChange(val === 'none' ? '' : val);
                    }}
                  >
                    <SelectTrigger size="sm" className="w-full bg-card text-xs" aria-label="Project">
                      <SelectValue placeholder="Select project..." />
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
                ) : (
                  <Input
                    id="task-project-input"
                    aria-label="Project"
                    value={field.state.value}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder="Project UUID"
                    className="text-xs font-mono"
                  />
                )}
              </div>
            )}
          />

          <form.Field
            name="model"
            children={(field) => (
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-foreground">Model</label>
                <Select
                  items={modelItems}
                  value={field.state.value}
                  onValueChange={(val) => {
                    if (typeof val === 'string') field.handleChange(val);
                  }}
                >
                  <SelectTrigger size="sm" className="w-full bg-card text-xs" aria-label="Model">
                    <SelectValue placeholder="Select model..." />
                  </SelectTrigger>
                  <SelectContent align="start">
                    <SelectGroup>
                      {modelItems.map((item) => (
                        <SelectItem key={item.value} value={item.value}>
                          {item.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </div>
            )}
          />
        </div>

        {projectIntegrations.length > 0 && (
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <form.Field
              name="integration_id"
              children={(field) => (
                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-foreground">Repository</label>
                  {projectIntegrations.length === 1 ? (
                    <div className="flex h-8 items-center truncate rounded-md border border-input bg-card px-2.5 font-mono text-xs text-foreground">
                      {projectIntegrations[0].external_id}
                    </div>
                  ) : (
                    <Select
                      items={repoItems}
                      value={integrationId}
                      onValueChange={(val) => {
                        if (typeof val !== 'string') return;
                        field.handleChange(val);
                        // A branch from the previous repository does not carry over.
                        form.setFieldValue('base_branch', '');
                      }}
                    >
                      <SelectTrigger size="sm" className="w-full bg-card text-xs font-mono" aria-label="Repository">
                        <SelectValue placeholder="Select repository..." />
                      </SelectTrigger>
                      <SelectContent align="start">
                        <SelectGroup>
                          {repoItems.map((item) => (
                            <SelectItem key={item.value} value={item.value} className="font-mono text-xs">
                              {item.label}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  )}
                </div>
              )}
            />

            <form.Field
              name="base_branch"
              children={(field) => (
                <div className="space-y-1.5">
                  <label htmlFor="target-base-branch" className="text-xs font-medium text-foreground">
                    Base branch
                  </label>
                  <Select
                    items={branchItems}
                    value={baseBranch}
                    onValueChange={(val) => {
                      if (typeof val === 'string') field.handleChange(val);
                    }}
                  >
                    <SelectTrigger
                      id="target-base-branch"
                      size="sm"
                      className="w-full bg-card text-xs font-mono"
                      aria-label="Base branch"
                      data-testid="target-branch-select"
                    >
                      <SelectValue placeholder="Select branch..." />
                    </SelectTrigger>
                    <SelectContent align="start">
                      <SelectGroup>
                        {branchItems.map((item) => (
                          <SelectItem key={item.value} value={item.value} className="font-mono text-xs">
                            {item.label}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </div>
              )}
            />
          </div>
        )}

        <form.Field
          name="title"
          children={(field) => {
            const error = errorText(field.state.meta.errors);
            return (
              <div className="space-y-1.5">
                <label htmlFor="task-title" className="text-xs font-medium text-foreground">
                  Title
                </label>
                <Input
                  id="task-title"
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  placeholder="Short summary of the task"
                  className="text-xs"
                  autoFocus
                />
                {error && <p className="text-[11px] text-destructive">{error}</p>}
              </div>
            );
          }}
        />

        <form.Field
          name="prompt"
          children={(field) => {
            const error = errorText(field.state.meta.errors);
            return (
              <div className="space-y-1.5">
                <div className="flex items-center justify-between">
                  <label htmlFor="task-prompt" className="text-xs font-medium text-foreground">
                    Prompt
                  </label>
                  <div className="flex items-center gap-1.5">
                    <input
                      ref={imageInputRef}
                      type="file"
                      accept={STEER_IMAGE_TYPES.join(',')}
                      multiple
                      className="hidden"
                      data-testid="task-image-input"
                      onChange={(e) => {
                        void attachImages(Array.from(e.target.files ?? []));
                        e.target.value = '';
                      }}
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => imageInputRef.current?.click()}
                      disabled={isSubmitting || images.length >= MAX_STEER_IMAGES}
                      className="size-6 text-muted-foreground hover:text-foreground"
                      aria-label="Attach images"
                      title={`Attach images (PNG, JPEG, GIF or WebP, up to ${MAX_STEER_IMAGES})`}
                      data-testid="attach-task-image-btn"
                    >
                      <ImagePlus className="size-3.5" />
                    </Button>
                    {images.length > 0 && (
                      <span className="text-[11px] text-muted-foreground tabular-nums">
                        {images.length}/{MAX_STEER_IMAGES}
                      </span>
                    )}
                  </div>
                </div>

                {imageError && (
                  <div role="alert" className="flex items-start gap-1.5 rounded-md border border-destructive/20 bg-destructive/10 px-2 py-1.5 text-xs text-destructive">
                    <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
                    <span>{imageError}</span>
                  </div>
                )}

                {images.length > 0 && (
                  <div className="flex flex-wrap gap-2 pt-1 pb-1.5" data-testid="task-image-list">
                    {images.map((img) => (
                      <div key={img.id} className="relative size-14 overflow-hidden rounded-lg border border-border bg-muted/30">
                        <img src={img.preview} alt={img.name} className="size-full object-cover" />
                        <button
                          type="button"
                          onClick={() => removeImage(img.id)}
                          aria-label={`Remove ${img.name}`}
                          className="absolute right-1 top-1 rounded-full bg-background/90 p-0.5 text-muted-foreground shadow-sm hover:text-foreground cursor-pointer"
                        >
                          <X className="size-3" />
                        </button>
                      </div>
                    ))}
                  </div>
                )}

                <Textarea
                  id="task-prompt"
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  onPaste={(e) => {
                    const files = Array.from(e.clipboardData?.files ?? []);
                    if (files.some((f) => f.type.startsWith('image/'))) {
                      e.preventDefault();
                      void attachImages(files);
                    }
                  }}
                  placeholder="What should the agent do? Paste or attach screenshots for reference."
                  rows={5}
                  className="min-h-[120px] text-xs"
                />
                {error && <p className="text-[11px] text-destructive">{error}</p>}
              </div>
            );
          }}
        />

        <DialogFooter className="pt-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              resetDetails();
              onOpenChange(false);
            }}
          >
            Cancel
          </Button>
          <Button type="submit" size="sm" disabled={isSubmitting}>
            {isSubmitting ? 'Creating...' : 'Dispatch task'}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  );
};
