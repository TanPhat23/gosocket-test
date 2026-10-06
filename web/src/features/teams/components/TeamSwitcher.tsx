import React, { useEffect } from 'react';
import { useTeamStore, useTeamsList, useActiveTeamId } from '../store/useTeamStore';
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

  useEffect(() => {
    if (teams.length === 0) {
      fetchTeams();
    }
  }, [teams.length, fetchTeams]);

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
