import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { SidebarProvider } from '@/components/ui/sidebar';
import { TooltipProvider } from '@/components/ui/tooltip';
import { useTeamStore } from '@/features/teams/store/useTeamStore';
import { useTaskStore } from '@/features/tasks/store/useTaskStore';
import { AppSidebar } from './AppSidebar';

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children, to, params: _params, ...rest }: { children?: React.ReactNode; to?: string; params?: unknown }) => (
    <a href={to} {...rest}>
      {children}
    </a>
  ),
}));
vi.mock('@/features/teams/components/TeamSwitcher', () => ({ TeamSwitcher: () => <div>Core Team</div> }));
vi.mock('@/features/teams/hooks/useWarmProjectRuntime', () => ({ useWarmProjectRuntime: () => undefined }));

const project = (id: string, name: string) => ({ id, name, team_id: 'team-1', created_at: '', updated_at: '' });

function renderSidebar() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(['integrations', 'p1'], [{ id: 'i1' }, { id: 'i2' }]);
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <SidebarProvider>
          <AppSidebar />
        </SidebarProvider>
      </TooltipProvider>
    </QueryClientProvider>
  );
}

describe('AppSidebar', () => {
  beforeEach(() => {
    useTaskStore.setState({ tasks: {} });
    useTeamStore.setState({
      teams: [],
      projects: [project('p1', 'Core Workspace'), project('p2', 'Docs')],
      activeTeamId: 'team-1',
      activeProjectId: 'p1',
      isLoading: false,
      error: null,
    });
  });

  it('lists the projects and the active one links to its container and repos', () => {
    renderSidebar();

    expect(screen.getByTestId('sidebar-all-projects')).toBeInTheDocument();
    expect(screen.getByTestId('sidebar-project-p1')).toHaveAttribute('aria-current', 'page');
    expect(screen.getByTestId('sidebar-project-p2')).not.toHaveAttribute('aria-current');
    expect(screen.getByTestId('sidebar-runtime-link')).toBeInTheDocument();
    expect(screen.getByTestId('sidebar-github-integrations-btn')).toBeInTheDocument();
    expect(screen.getByTestId('sidebar-repos-count')).toHaveTextContent('2');
  });

  it('switches the active project, and to all projects', () => {
    renderSidebar();

    fireEvent.click(screen.getByTestId('sidebar-project-p2'));
    expect(useTeamStore.getState().activeProjectId).toBe('p2');

    fireEvent.click(screen.getByTestId('sidebar-all-projects'));
    expect(useTeamStore.getState().activeProjectId).toBeNull();
  });

  it('shows what a project is working on', () => {
    useTaskStore.setState({
      tasks: { t1: { id: 't1', project_id: 'p1', title: 'Fix login', status: 'in_progress' } } as never,
    });
    renderSidebar();

    expect(screen.getByTestId('project-active-task-p1')).toHaveTextContent('Fix login');
  });

  it('opens the new-project dialog', () => {
    renderSidebar();

    fireEvent.click(screen.getByTestId('add-new-project-btn'));

    expect(screen.getByTestId('new-project-name-input')).toBeInTheDocument();
  });
});
