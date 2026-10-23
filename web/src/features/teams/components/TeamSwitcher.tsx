import React, { useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useTeamStore, useTeamsList, useActiveTeamId } from '../store/useTeamStore';
import { teamsQueryOptions } from '../queries';
import { SearchableSelect } from '@/components/SearchableSelect';
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
      <SearchableSelect
        items={teams.map((team) => ({ value: team.id, label: team.name }))}
        value={activeTeamId ?? ''}
        onValueChange={selectTeam}
        disabled={isLoading || teams.length === 0}
        placeholder="Select team..."
        searchPlaceholder="Search teams..."
        emptyText="No teams found."
        icon={<Users className="size-3.5 text-muted-foreground shrink-0" />}
        ariaLabel="Team"
        testId="team-switcher"
      />
    </div>
  );
};
