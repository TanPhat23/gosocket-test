import React, { useState } from 'react';
import { useTaskStore } from '../store/useTaskStore';
import { useProjectsList, useActiveProjectId } from '@/features/teams/store/useTeamStore';
import { CreateTaskRequestSchema } from '../schemas/taskSchemas';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  DialogFooter,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Plus, Check, Sparkles, Server, Wrench, AlertCircle } from 'lucide-react';
import { cn } from '@/lib/utils';

const AVAILABLE_MODELS = [
  'claude-3-7-sonnet',
  'claude-3-5-sonnet',
  'gpt-4o',
  'o3-mini',
  'gemini-2.5-pro',
];

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

export interface TaskModalProps {
  trigger?: React.ReactNode;
}

export const TaskModal: React.FC<TaskModalProps> = ({ trigger }) => {
  const [open, setOpen] = useState(false);
  const createTask = useTaskStore((state) => state.createTask);
  const projects = useProjectsList();
  const activeProjectId = useActiveProjectId();

  const [projectId, setProjectId] = useState<string>(activeProjectId || (projects[0]?.id ?? 'default-project'));
  const [title, setTitle] = useState('');
  const [prompt, setPrompt] = useState('');
  const [priority, setPriority] = useState<number>(50);
  const [selectedModel, setSelectedModel] = useState<string>('claude-3-7-sonnet');
  const [selectedSkills, setSelectedSkills] = useState<string[]>([]);
  const [selectedMcps, setSelectedMcps] = useState<string[]>([]);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [isSubmitting, setIsSubmitting] = useState(false);

  // Sync project ID when projects load if needed
  React.useEffect(() => {
    if (!projectId && activeProjectId) {
      setProjectId(activeProjectId);
    } else if (!projectId && projects.length > 0) {
      setProjectId(projects[0].id);
    }
  }, [activeProjectId, projects, projectId]);

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
    setPriority(50);
    setSelectedSkills([]);
    setSelectedMcps([]);
    setErrors({});
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrors({});

    const rawData = {
      project_id: projectId || 'default-project',
      title: title.trim(),
      prompt: prompt.trim(),
      assignee_id: 'agent-sandbox-1',
      assignee_type: 'agent' as const,
      priority: Number(priority),
      model: selectedModel,
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

    try {
      setIsSubmitting(true);
      await createTask(validation.data);
      resetForm();
      setOpen(false);
    } catch (err) {
      setErrors({ form: err instanceof Error ? err.message : 'Failed to create task' });
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger
        render={
          trigger ? (
            (trigger as React.ReactElement)
          ) : (
            <Button size="sm" className="gap-1.5 shadow-xs font-medium">
              <Plus className="size-4" />
              New Task
            </Button>
          )
        }
      />

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
            <label className="text-xs font-semibold text-foreground">Project</label>
            {projects.length > 0 ? (
              <select
                value={projectId}
                onChange={(e) => setProjectId(e.target.value)}
                className="w-full h-8 text-xs rounded-lg border border-input bg-transparent px-2.5 py-1 text-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 outline-none"
              >
                {projects.map((proj) => (
                  <option key={proj.id} value={proj.id}>
                    {proj.name}
                  </option>
                ))}
              </select>
            ) : (
              <Input
                value={projectId}
                onChange={(e) => setProjectId(e.target.value)}
                placeholder="project-id"
                className="text-xs font-mono"
              />
            )}
            {errors.project_id && (
              <p className="text-[11px] text-destructive">{errors.project_id}</p>
            )}
          </div>

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

            {/* Model */}
            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-foreground">Model Engine</label>
              <select
                value={selectedModel}
                onChange={(e) => setSelectedModel(e.target.value)}
                className="w-full h-8 text-xs rounded-lg border border-input bg-transparent px-2.5 py-1 text-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 outline-none"
              >
                {AVAILABLE_MODELS.map((model) => (
                  <option key={model} value={model}>
                    {model}
                  </option>
                ))}
              </select>
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
                setOpen(false);
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
    </Dialog>
  );
};
