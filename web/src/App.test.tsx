import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { App } from './App';
import { ThemeProvider } from '@/components/theme/ThemeProvider';
import { useAuthStore } from '@/features/auth/store/useAuthStore';
import { useTaskStore } from '@/features/tasks/store/useTaskStore';
import { useTeamStore } from '@/features/teams/store/useTeamStore';

// Mock subcomponents/hooks that may interact with network or websockets
vi.mock('@/features/tasks/hooks/useTaskSubscription', () => ({
  useTaskSubscription: vi.fn(),
}));

const renderWithProviders = (ui: React.ReactElement) => {
  return render(<ThemeProvider>{ui}</ThemeProvider>);
};

describe('App Auth Gateway', () => {
  const fetchUserMock = vi.fn();
  const fetchTasksMock = vi.fn();
  const fetchTeamsMock = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();

    useTaskStore.setState({
      tasks: {},
      isLoading: false,
      error: null,
      fetchTasks: fetchTasksMock,
    });

    useTeamStore.setState({
      teams: [],
      activeTeamId: null,
      projects: [],
      activeProjectId: null,
      isLoading: false,
      error: null,
      fetchTeams: fetchTeamsMock,
    });
  });

  it('renders loading state ("Verifying session...") when loading and unauthenticated', () => {
    useAuthStore.setState({
      user: null,
      isLoading: true,
      isInitialized: false,
      error: null,
      fetchUser: fetchUserMock,
    });

    renderWithProviders(<App />);

    expect(fetchUserMock).toHaveBeenCalledTimes(1);
    expect(screen.getByText('Verifying session...')).toBeInTheDocument();
    expect(screen.queryByText(/Agent Control Plane/i)).not.toBeInTheDocument();
    expect(fetchTasksMock).not.toHaveBeenCalled();
    expect(fetchTeamsMock).not.toHaveBeenCalled();
  });

  it('triggers auto-redirect to /api/v1/auth/login and renders "Redirecting to sign-in..." when unauthenticated', () => {
    // Set up location mock
    const locationHrefSpy = vi.fn();
    const originalLocation = window.location;

    // In jsdom, window.location can be redefined or spied on
    Object.defineProperty(window, 'location', {
      writable: true,
      value: {
        ...originalLocation,
        get href() {
          return 'http://localhost/';
        },
        set href(val: string) {
          locationHrefSpy(val);
        },
      },
    });

    try {
      useAuthStore.setState({
        user: null,
        isLoading: false,
        isInitialized: true,
        error: null,
        fetchUser: fetchUserMock,
      });

      renderWithProviders(<App />);

      expect(fetchUserMock).toHaveBeenCalledTimes(1);
      expect(locationHrefSpy).toHaveBeenCalledWith('/api/v1/auth/login');
      expect(screen.getByText('Redirecting to sign-in...')).toBeInTheDocument();
      expect(screen.queryByText(/Agent Control Plane/i)).not.toBeInTheDocument();
      expect(fetchTasksMock).not.toHaveBeenCalled();
      expect(fetchTeamsMock).not.toHaveBeenCalled();
    } finally {
      Object.defineProperty(window, 'location', {
        writable: true,
        value: originalLocation,
      });
    }
  });

  it('renders authenticated workspace when user is logged in and triggers task/team fetches', () => {
    useAuthStore.setState({
      user: {
        id: 'usr-123',
        email: 'dev@example.com',
        display_name: 'Lead Developer',
      },
      isLoading: false,
      isInitialized: true,
      error: null,
      fetchUser: fetchUserMock,
    });

    renderWithProviders(<App />);

    expect(fetchUserMock).toHaveBeenCalledTimes(1);
    expect(screen.getByText('Agent Control Plane')).toBeInTheDocument();
    expect(fetchTasksMock).toHaveBeenCalledTimes(1);
    expect(fetchTeamsMock).toHaveBeenCalled();
  });
});
