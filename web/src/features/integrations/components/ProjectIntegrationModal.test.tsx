import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ProjectIntegrationModal } from './ProjectIntegrationModal';
import { useTeamStore } from '@/features/teams/store/useTeamStore';
import { integrationApi } from '../api/integrationApi';
import { credentialApi } from '@/features/teams/api/credentialApi';

describe('ProjectIntegrationModal', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useTeamStore.setState({
      activeProjectId: 'proj-1',
      activeTeamId: 'team-1',
      projects: [
        {
          id: 'proj-1',
          team_id: 'team-1',
          name: 'ATM Frontend',
        },
      ],
    });
  });

  it('renders trigger button and opens modal with connected repos', async () => {
    vi.spyOn(integrationApi, 'listProjectIntegrations').mockResolvedValue([
      {
        id: 'integ-1',
        project_id: 'proj-1',
        credential_id: 'cred-1',
        provider: 'github',
        external_id: 'test-org/repo-a',
        external_url: 'https://github.com/test-org/repo-a',
        enabled: true,
        config: {},
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    ]);
    vi.spyOn(credentialApi, 'listCredentials').mockResolvedValue([
      {
        id: 'cred-1',
        team_id: 'team-1',
        provider: 'github',
        label: 'Org GitHub PAT',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    ]);

    render(<ProjectIntegrationModal />);

    const triggerBtn = screen.getByTestId('integrations-trigger-button');
    expect(triggerBtn).toBeInTheDocument();
    fireEvent.click(triggerBtn);

    await waitFor(() => {
      expect(screen.getByText('Project Integrations')).toBeInTheDocument();
      expect(screen.getByText('test-org/repo-a')).toBeInTheDocument();
      expect(screen.getByText('Active')).toBeInTheDocument();
    });
  });

  it('validates repository format before connecting repository', async () => {
    vi.spyOn(integrationApi, 'listProjectIntegrations').mockResolvedValue([]);
    vi.spyOn(credentialApi, 'listCredentials').mockResolvedValue([
      {
        id: 'cred-1',
        team_id: 'team-1',
        provider: 'github',
        label: 'Default PAT',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    ]);

    render(<ProjectIntegrationModal />);

    fireEvent.click(screen.getByTestId('integrations-trigger-button'));

    await waitFor(() => {
      expect(screen.getByTestId('toggle-connect-repo-btn')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('toggle-connect-repo-btn'));

    expect(screen.getByTestId('connect-repo-form')).toBeInTheDocument();

    const submitBtn = screen.getByTestId('connect-submit-btn');
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(screen.getByText('Repository is required')).toBeInTheDocument();
    });

    const repoInput = screen.getByTestId('repo-input');
    fireEvent.change(repoInput, { target: { value: 'invalid-repo-without-slash' } });
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(screen.getByText('Format must be owner/repo (e.g. facebook/react)')).toBeInTheDocument();
    });
  });

  it('successfully creates an integration and displays it in list', async () => {
    vi.spyOn(integrationApi, 'listProjectIntegrations').mockResolvedValue([]);
    vi.spyOn(credentialApi, 'listCredentials').mockResolvedValue([
      {
        id: 'cred-1',
        team_id: 'team-1',
        provider: 'github',
        label: 'Default PAT',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    ]);

    const createSpy = vi.spyOn(integrationApi, 'createProjectIntegration').mockResolvedValue({
      id: 'integ-created-1',
      project_id: 'proj-1',
      credential_id: 'cred-1',
      provider: 'github',
      external_id: 'org/new-repo',
      external_url: 'https://github.com/org/new-repo',
      enabled: true,
      config: {},
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    });

    render(<ProjectIntegrationModal />);

    fireEvent.click(screen.getByTestId('integrations-trigger-button'));

    await waitFor(() => {
      expect(screen.getByTestId('toggle-connect-repo-btn')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('toggle-connect-repo-btn'));

    fireEvent.change(screen.getByTestId('repo-input'), {
      target: { value: 'org/new-repo' },
    });

    fireEvent.click(screen.getByTestId('connect-submit-btn'));

    await waitFor(() => {
      expect(createSpy).toHaveBeenCalledWith(
        'proj-1',
        expect.objectContaining({
          repo: 'org/new-repo',
          credential_id: 'cred-1',
          auto_pr: true,
          base_branch: 'main',
        })
      );
      expect(screen.getByText('org/new-repo')).toBeInTheDocument();
    });
  });

  it('allows deleting an integration after confirmation', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    const deleteSpy = vi.spyOn(integrationApi, 'deleteProjectIntegration').mockResolvedValue();

    vi.spyOn(integrationApi, 'listProjectIntegrations').mockResolvedValue([
      {
        id: 'integ-to-delete',
        project_id: 'proj-1',
        credential_id: 'cred-1',
        provider: 'github',
        external_id: 'org/to-delete',
        external_url: 'https://github.com/org/to-delete',
        enabled: true,
        config: {},
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    ]);
    vi.spyOn(credentialApi, 'listCredentials').mockResolvedValue([]);

    render(<ProjectIntegrationModal />);

    fireEvent.click(screen.getByTestId('integrations-trigger-button'));

    await waitFor(() => {
      expect(screen.getByTestId('delete-integration-integ-to-delete')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('delete-integration-integ-to-delete'));

    await waitFor(() => {
      expect(deleteSpy).toHaveBeenCalledWith('proj-1', 'integ-to-delete');
      expect(screen.queryByText('org/to-delete')).not.toBeInTheDocument();
    });
  });
});
