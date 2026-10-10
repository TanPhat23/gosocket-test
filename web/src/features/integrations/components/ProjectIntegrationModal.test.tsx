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

  it('renders repository picker and selects a repo when clicked', async () => {
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
    vi.spyOn(credentialApi, 'listGitHubRepositories').mockResolvedValue([
      {
        id: 123,
        name: 'agent-repo',
        full_name: 'test-org/agent-repo',
        private: true,
        description: 'Autonomous agent core',
        html_url: 'https://github.com/test-org/agent-repo',
        default_branch: 'develop',
      },
    ]);

    render(<ProjectIntegrationModal />);

    fireEvent.click(screen.getByTestId('integrations-trigger-button'));

    await waitFor(() => {
      expect(screen.getByTestId('toggle-connect-repo-btn')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('toggle-connect-repo-btn'));

    await waitFor(() => {
      expect(screen.getByTestId('repo-item-test-org/agent-repo')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('repo-item-test-org/agent-repo'));

    const repoInput = screen.getByTestId('repo-input') as HTMLInputElement;
    expect(repoInput.value).toBe('test-org/agent-repo');
  });

  it('shows actionable banner on expired token error and switches to PAT edit on click', async () => {
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
        label: 'Expired Bot PAT',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    ]);
    vi.spyOn(credentialApi, 'listGitHubRepositories').mockRejectedValue(
      new Error('invalid or expired github token')
    );

    render(<ProjectIntegrationModal />);

    fireEvent.click(screen.getByTestId('integrations-trigger-button'));

    await waitFor(() => {
      expect(screen.getByTestId('token-error-banner')).toBeInTheDocument();
      expect(screen.getByText('Invalid or Expired GitHub Token')).toBeInTheDocument();
    });

    const fixBtn = screen.getByTestId('fix-token-btn');
    expect(fixBtn).toBeInTheDocument();
    fireEvent.click(fixBtn);

    await waitFor(() => {
      expect(screen.getByTestId('token-edit-form')).toBeInTheDocument();
      expect(screen.getByTestId('token-secret-input')).toBeInTheDocument();
    });
  });

  it('updates an existing token via credentialApi.updateCredential', async () => {
    vi.spyOn(integrationApi, 'listProjectIntegrations').mockResolvedValue([]);
    vi.spyOn(credentialApi, 'listCredentials').mockResolvedValue([
      {
        id: 'cred-1',
        team_id: 'team-1',
        provider: 'github',
        label: 'Existing Bot PAT',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    ]);
    vi.spyOn(credentialApi, 'listGitHubRepositories').mockResolvedValue([]);

    const updateSpy = vi.spyOn(credentialApi, 'updateCredential').mockResolvedValue({
      id: 'cred-1',
      team_id: 'team-1',
      provider: 'github',
      label: 'Updated Bot PAT',
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    });

    render(<ProjectIntegrationModal />);

    fireEvent.click(screen.getByTestId('integrations-trigger-button'));

    await waitFor(() => {
      expect(screen.getByTestId('tab-pat')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('tab-pat'));

    await waitFor(() => {
      expect(screen.getByTestId('edit-token-btn-cred-1')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('edit-token-btn-cred-1'));

    expect(screen.getByTestId('token-edit-form')).toBeInTheDocument();

    fireEvent.change(screen.getByTestId('token-label-input'), {
      target: { value: 'Updated Bot PAT' },
    });
    fireEvent.change(screen.getByTestId('token-secret-input'), {
      target: { value: 'ghp_brand_new_secret_pat' },
    });

    fireEvent.click(screen.getByTestId('save-token-btn'));

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith('team-1', 'cred-1', {
        label: 'Updated Bot PAT',
        secret: 'ghp_brand_new_secret_pat',
      });
    });
  });

  it('tests connection for a token via credentialApi.listGitHubRepositories', async () => {
    vi.spyOn(integrationApi, 'listProjectIntegrations').mockResolvedValue([]);
    vi.spyOn(credentialApi, 'listCredentials').mockResolvedValue([
      {
        id: 'cred-1',
        team_id: 'team-1',
        provider: 'github',
        label: 'Test Bot PAT',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    ]);
    const listSpy = vi.spyOn(credentialApi, 'listGitHubRepositories').mockResolvedValue([
      {
        id: 1,
        name: 'test-repo',
        full_name: 'org/test-repo',
        private: false,
        description: '',
        html_url: 'https://github.com/org/test-repo',
        default_branch: 'main',
      },
    ]);

    render(<ProjectIntegrationModal />);

    fireEvent.click(screen.getByTestId('integrations-trigger-button'));

    await waitFor(() => {
      expect(screen.getByTestId('tab-pat')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('tab-pat'));

    await waitFor(() => {
      expect(screen.getByTestId('test-token-btn-cred-1')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('test-token-btn-cred-1'));

    await waitFor(() => {
      expect(listSpy).toHaveBeenCalledWith('team-1', 'cred-1');
      expect(screen.getByText(/Token verified successfully! Found 1 accessible repositories/)).toBeInTheDocument();
    });
  });
});
