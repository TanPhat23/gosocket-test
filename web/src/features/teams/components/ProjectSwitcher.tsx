import React from 'react';
import { useTeamStore, useProjectsList, useActiveProjectId } from '../store/useTeamStore';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { FolderGit2 } from 'lucide-react';

export const ProjectSwitcher: React.FC = () => {
  const projects = useProjectsList();
  const activeProjectId = useActiveProjectId();
  const selectProject = useTeamStore((state) => state.selectProject);
  const isLoading = useTeamStore((state) => state.isLoading);

  if (projects.length === 0 && !isLoading) {
    return null;
  }

  return (
    <div className="flex items-center gap-2">
      <Select
        value={activeProjectId ?? ''}
        onValueChange={(val) => {
          if (val && typeof val === 'string') {
            selectProject(val);
          }
        }}
        disabled={isLoading || projects.length === 0}
      >
        <SelectTrigger size="sm" className="w-[180px] bg-background/50">
          <FolderGit2 className="size-3.5 text-muted-foreground mr-1 shrink-0" />
          <SelectValue placeholder="Select project..." />
        </SelectTrigger>
        <SelectContent align="start">
          {projects.map((proj) => (
            <SelectItem key={proj.id} value={proj.id}>
              {proj.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
};
