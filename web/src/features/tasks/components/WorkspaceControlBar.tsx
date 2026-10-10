import React, { useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useWorkspaceSettingsStore } from '../store/useWorkspaceSettingsStore';
import { useActiveProjectId } from '@/features/teams/store/useTeamStore';
import { useTaskStore } from '../store/useTaskStore';
import { TaskModal } from './TaskModal';
import { WorkspaceSettingsModal } from './WorkspaceSettingsModal';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  Search,
  GitBranch,
  Cpu,
  Filter,
  X,
} from 'lucide-react';
import { modelsQueryOptions } from '../queries';
import { projectIntegrationsQueryOptions } from '@/features/integrations/queries';

export const WorkspaceControlBar: React.FC = () => {
  const activeProjectId = useActiveProjectId();

  const defaultModel = useWorkspaceSettingsStore((s) => s.defaultModel);
  const setDefaultModel = useWorkspaceSettingsStore((s) => s.setDefaultModel);
  const availableModels = useWorkspaceSettingsStore((s) => s.availableModels);
  const setAvailableModels = useWorkspaceSettingsStore((s) => s.setAvailableModels);

  const selectedRepoId = useWorkspaceSettingsStore((s) => s.selectedRepoId);
  const setSelectedRepoId = useWorkspaceSettingsStore((s) => s.setSelectedRepoId);
  const setConnectedRepos = useWorkspaceSettingsStore((s) => s.setConnectedRepos);
  const defaultRepoId = useWorkspaceSettingsStore((s) => s.defaultRepoId);

  const searchQuery = useWorkspaceSettingsStore((s) => s.searchQuery);
  const setSearchQuery = useWorkspaceSettingsStore((s) => s.setSearchQuery);
  const priorityFilter = useWorkspaceSettingsStore((s) => s.priorityFilter);
  const setPriorityFilter = useWorkspaceSettingsStore((s) => s.setPriorityFilter);

  const tasks = useTaskStore((s) => s.tasks);
  const taskIds = useTaskStore((s) => s.taskIds);

  // Queries for models and project integrations
  const { data: modelsData } = useQuery(modelsQueryOptions());
  const { data: integrationsData } = useQuery({
    ...projectIntegrationsQueryOptions(activeProjectId || ''),
    enabled: Boolean(activeProjectId),
  });

  useEffect(() => {
    if (modelsData && modelsData.length > 0) {
      setAvailableModels(modelsData);
    }
  }, [modelsData, setAvailableModels]);

  useEffect(() => {
    if (!activeProjectId) {
      setConnectedRepos([]);
      setSelectedRepoId('all');
      return;
    }
    if (integrationsData) {
      const ghIntegs = integrationsData.filter((i) => i.provider === 'github' && i.enabled);
      setConnectedRepos(ghIntegs);
      if (selectedRepoId !== 'all' && ghIntegs.some((g) => g.id === selectedRepoId)) {
        return;
      }
      if (defaultRepoId && ghIntegs.some((g) => g.id === defaultRepoId)) {
        setSelectedRepoId(defaultRepoId);
        return;
      }
      if (ghIntegs.length > 0 && selectedRepoId === 'all') {
        // keep 'all' or default
      }
    }
  }, [activeProjectId, integrationsData, selectedRepoId, defaultRepoId, setConnectedRepos, setSelectedRepoId]);

  const connectedRepos = useWorkspaceSettingsStore((s) => s.connectedRepos);

  // Calculate filtered task count
  const matchingTasksCount = taskIds.filter((id) => {
    const t = tasks[id];
    if (!t) return false;
    if (activeProjectId && t.project_id && t.project_id !== activeProjectId) return false;
    if (selectedRepoId && selectedRepoId !== 'all' && t.integration_id !== selectedRepoId) return false;
    if (priorityFilter === 'high' && t.priority < 70) return false;
    if (priorityFilter === 'normal' && (t.priority < 40 || t.priority >= 70)) return false;
    if (priorityFilter === 'low' && t.priority >= 40) return false;
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase().trim();
      const matchTitle = t.title?.toLowerCase().includes(q);
      const matchPrompt = t.prompt?.toLowerCase().includes(q);
      const matchId = t.id?.toLowerCase().includes(q);
      if (!matchTitle && !matchPrompt && !matchId) return false;
    }
    return true;
  }).length;

  const hasActiveFilters = Boolean(
    searchQuery.trim() ||
    priorityFilter !== 'all' ||
    (selectedRepoId && selectedRepoId !== 'all')
  );

  const handleResetFilters = () => {
    setSearchQuery('');
    setPriorityFilter('all');
    setSelectedRepoId('all');
  };

  const selectedRepo = connectedRepos.find((r) => r.id === selectedRepoId);

  return (
    <div className="flex flex-col gap-3.5 bg-card/60 backdrop-blur border border-border/80 rounded-xl p-4 shadow-xs">
      {/* Top Header Row */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <h1 className="text-base font-bold tracking-tight text-foreground">
              Agent Control Plane
            </h1>
            <Badge variant="outline" className="text-[10px] font-mono px-1.5 py-0">
              {matchingTasksCount} {matchingTasksCount === 1 ? 'task' : 'tasks'}
            </Badge>
          </div>
          <p className="text-xs text-muted-foreground mt-0.5">
            Autonomous agent task executions, isolated worktrees, and streaming diagnostics.
          </p>
        </div>

        <div className="flex items-center gap-2 self-start sm:self-auto">
          <WorkspaceSettingsModal />
          <TaskModal />
        </div>
      </div>

      {/* Repository & Model Selector Control Panel */}
      <div className="grid grid-cols-1 sm:grid-cols-12 gap-2.5 pt-1">
        {/* Target Repository Selector / Filter */}
        <div className="sm:col-span-3">
          <Select
            value={selectedRepoId || 'all'}
            onValueChange={(val) => {
              if (typeof val === 'string') {
                setSelectedRepoId(val);
              }
            }}
          >
            <SelectTrigger size="sm" className="w-full bg-background/80 text-xs font-mono" aria-label="Filter by repository">
              <GitBranch className="size-3.5 text-primary mr-1 shrink-0" />
              <SelectValue placeholder="All Repositories" />
            </SelectTrigger>
            <SelectContent align="start">
              <SelectItem value="all">
                All Repositories {connectedRepos.length > 0 ? `(${connectedRepos.length})` : ''}
              </SelectItem>
              {connectedRepos.map((r) => (
                <SelectItem key={r.id} value={r.id}>
                  {r.external_id} ({String(r.config?.base_branch || 'main')})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {/* Model Selector / Switcher */}
        <div className="sm:col-span-3">
          <Select
            value={defaultModel}
            onValueChange={(val) => {
              if (typeof val === 'string') {
                setDefaultModel(val);
              }
            }}
          >
            <SelectTrigger size="sm" className="w-full bg-background/80 text-xs font-mono" aria-label="Default AI Model">
              <Cpu className="size-3.5 text-primary mr-1 shrink-0" />
              <SelectValue placeholder="Select model..." />
            </SelectTrigger>
            <SelectContent align="start">
              {availableModels.map((m) => (
                <SelectItem key={m.id} value={m.id}>
                  {m.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {/* Search Task Input */}
        <div className="sm:col-span-4 relative flex items-center">
          <Search className="absolute left-2.5 size-3.5 text-muted-foreground pointer-events-none" />
          <Input
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="Filter tasks by title, ID, or prompt..."
            className="h-8 pl-8 pr-7 text-xs bg-background/80"
          />
          {searchQuery && (
            <button
              type="button"
              onClick={() => setSearchQuery('')}
              className="absolute right-2 text-muted-foreground hover:text-foreground p-0.5 cursor-pointer"
              title="Clear search"
            >
              <X className="size-3" />
            </button>
          )}
        </div>

        {/* Priority Filter */}
        <div className="sm:col-span-2">
          <Select
            value={priorityFilter}
            onValueChange={(val) => {
              if (typeof val === 'string') {
                setPriorityFilter(val);
              }
            }}
          >
            <SelectTrigger size="sm" className="w-full bg-background/80 text-xs" aria-label="Filter by priority">
              <Filter className="size-3.5 text-muted-foreground mr-1 shrink-0" />
              <SelectValue placeholder="All Priorities" />
            </SelectTrigger>
            <SelectContent align="start">
              <SelectItem value="all">All Priorities</SelectItem>
              <SelectItem value="high">High (P70+)</SelectItem>
              <SelectItem value="normal">Normal (P40-69)</SelectItem>
              <SelectItem value="low">Low (&lt;P40)</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      {/* Active Filter Indicators */}
      {hasActiveFilters && (
        <div className="flex items-center justify-between text-xs pt-1 px-1 text-muted-foreground border-t border-border/40">
          <div className="flex items-center gap-1.5 flex-wrap">
            <span className="text-[11px] font-medium text-foreground">Active Filters:</span>
            {selectedRepo && (
              <Badge variant="secondary" className="text-[10px] gap-1 px-1.5 py-0 font-mono">
                Repo: {selectedRepo.external_id}
                <X className="size-2.5 cursor-pointer hover:text-foreground" onClick={() => setSelectedRepoId('all')} />
              </Badge>
            )}
            {searchQuery && (
              <Badge variant="secondary" className="text-[10px] gap-1 px-1.5 py-0 font-normal">
                Query: "{searchQuery}"
                <X className="size-2.5 cursor-pointer hover:text-foreground" onClick={() => setSearchQuery('')} />
              </Badge>
            )}
            {priorityFilter !== 'all' && (
              <Badge variant="secondary" className="text-[10px] gap-1 px-1.5 py-0 font-normal">
                Priority: {priorityFilter}
                <X className="size-2.5 cursor-pointer hover:text-foreground" onClick={() => setPriorityFilter('all')} />
              </Badge>
            )}
          </div>

          <Button
            variant="ghost"
            size="sm"
            onClick={handleResetFilters}
            className="h-6 px-2 text-[11px] text-muted-foreground hover:text-foreground"
          >
            Reset Filters
          </Button>
        </div>
      )}
    </div>
  );
};
