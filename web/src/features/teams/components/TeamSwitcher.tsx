import React, { useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useTeamStore, useTeamsList, useActiveTeamId } from '../store/useTeamStore';
import { teamsQueryOptions } from '../queries';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Users } from 'lucide-react';

export const TeamSwitcher: React.FC = () => {
  const teams = useTeamsList();
  const activeTeamId = useActiveTeamId();
  const fetchTeams = useTeamStore((state) => state.fetchTeams);
  const selectTeam = useTeamStore((state) => state.selectTeam);
  const isLoading = useTeamStore((state) => state.isLoading);
  const queryClient = useQueryClient();

  useEffect(() => {
    // WorkspaceView already loads the teams query and copies it into the store. Fetch here only when that query has
    // no data, which avoids a second GET /teams on every page load.
    if (teams.length === 0 && queryClient.getQueryData(teamsQueryOptions().queryKey) === undefined) {
      fetchTeams();
    }
  }, [teams.length, fetchTeams, queryClient]);

  if (teams.length === 0 && !isLoading) {
    return null;
  }

  return (
    <div className="flex items-center gap-2">
      <Select
        value={activeTeamId ?? ''}
        onValueChange={(val) => {
          if (val && typeof val === 'string') {
            selectTeam(val);
          }
        }}
        disabled={isLoading || teams.length === 0}
      >
        <SelectTrigger size="sm" className="w-[180px] bg-background/50">
          <Users className="size-3.5 text-muted-foreground mr-1 shrink-0" />
          <SelectValue placeholder="Select team..." />
        </SelectTrigger>
        <SelectContent align="start">
          {teams.map((team) => (
            <SelectItem key={team.id} value={team.id}>
              {team.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
};
