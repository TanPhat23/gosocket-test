import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { TeamSwitcher } from './TeamSwitcher';
import { useTeamStore } from '../store/useTeamStore';
import { teamApi } from '../api/teamApi';
import { teamsQueryOptions } from '../queries';

describe('TeamSwitcher', () => {
  beforeEach(() => {
    useTeamStore.setState({ teams: [], activeTeamId: null, projects: [], activeProjectId: null, isLoading: false });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('does not request teams again when the teams query already has data', async () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(teamsQueryOptions().queryKey, [{ id: 't1', name: 'Team One' }]);
    const getTeams = vi.spyOn(teamApi, 'getTeams').mockResolvedValue([]);

    render(
      <QueryClientProvider client={queryClient}>
        <TeamSwitcher />
      </QueryClientProvider>
    );

    // Give any effect a chance to run before asserting.
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(getTeams).not.toHaveBeenCalled();
  });

  it('fetches teams when the teams query has no data', async () => {
    const queryClient = new QueryClient();
    const getTeams = vi
      .spyOn(teamApi, 'getTeams')
      .mockResolvedValue([{ id: 't2', name: 'Team Two' }] as never);

    render(
      <QueryClientProvider client={queryClient}>
        <TeamSwitcher />
      </QueryClientProvider>
    );

    await waitFor(() => expect(getTeams).toHaveBeenCalledTimes(1));
  });

  it('shows the active team name in the trigger, not its id', () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(teamsQueryOptions().queryKey, [{ id: 't1', name: 'Team One' }]);
    useTeamStore.setState({ teams: [{ id: 't1', name: 'Team One' }] as never, activeTeamId: 't1' });

    render(
      <QueryClientProvider client={queryClient}>
        <TeamSwitcher />
      </QueryClientProvider>
    );

    expect(screen.getByText('Team One')).toBeInTheDocument();
    expect(screen.queryByText('t1')).not.toBeInTheDocument();
  });
});
