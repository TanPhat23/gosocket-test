import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { authApi } from '../api/authApi';
import { useAuthStore } from '../store/useAuthStore';
import { GitIdentitySetting } from './GitIdentitySetting';

function renderSetting() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <GitIdentitySetting />
    </QueryClientProvider>
  );
}

const typeInto = (testId: string, value: string) => fireEvent.change(screen.getByTestId(testId), { target: { value } });

describe('GitIdentitySetting', () => {
  beforeEach(() => {
    useAuthStore.setState({ user: { id: 'u1', git_name: '', git_email: '' } });
  });
  afterEach(() => {
    vi.restoreAllMocks();
    useAuthStore.setState({ user: null });
  });

  it('starts from the saved identity and cannot save what has not changed', () => {
    useAuthStore.setState({ user: { id: 'u1', git_name: 'Jane Dev', git_email: 'jane@example.com' } });
    renderSetting();

    expect(screen.getByTestId('git-name-input')).toHaveValue('Jane Dev');
    expect(screen.getByTestId('git-email-input')).toHaveValue('jane@example.com');
    expect(screen.getByTestId('git-identity-save')).toBeDisabled();
  });

  it('saves the trimmed identity and stores the returned user', async () => {
    const set = vi.spyOn(authApi, 'setGitIdentity').mockResolvedValue({ id: 'u1', git_name: 'Jane Dev', git_email: 'jane@example.com' });
    renderSetting();

    typeInto('git-name-input', '  Jane Dev ');
    typeInto('git-email-input', 'jane@example.com');
    fireEvent.click(screen.getByTestId('git-identity-save'));

    await waitFor(() => expect(set).toHaveBeenCalledWith({ git_name: 'Jane Dev', git_email: 'jane@example.com' }));
    await waitFor(() => expect(screen.getByText('Saved.')).toBeInTheDocument());
    expect(useAuthStore.getState().user?.git_email).toBe('jane@example.com');
  });

  it('refuses a name without an email', () => {
    renderSetting();

    typeInto('git-name-input', 'Jane');

    expect(screen.getByTestId('git-identity-save')).toBeDisabled();
    expect(screen.getByText('Enter both a name and an email, or clear both.')).toBeInTheDocument();
  });

  it('refuses a malformed email', () => {
    renderSetting();

    typeInto('git-name-input', 'Jane');
    typeInto('git-email-input', 'jane');

    expect(screen.getByTestId('git-identity-save')).toBeDisabled();
    expect(screen.getByText('Enter a valid email address.')).toBeInTheDocument();
  });

  it('clears a saved identity when both fields are emptied', async () => {
    useAuthStore.setState({ user: { id: 'u1', git_name: 'Jane Dev', git_email: 'jane@example.com' } });
    const set = vi.spyOn(authApi, 'setGitIdentity').mockResolvedValue({ id: 'u1' });
    renderSetting();

    typeInto('git-name-input', '');
    typeInto('git-email-input', '');
    fireEvent.click(screen.getByTestId('git-identity-save'));

    await waitFor(() => expect(set).toHaveBeenCalledWith({ git_name: '', git_email: '' }));
  });

  it('shows the server error and keeps the form editable', async () => {
    vi.spyOn(authApi, 'setGitIdentity').mockRejectedValue(new Error('git email is not a valid address'));
    renderSetting();

    typeInto('git-name-input', 'Jane');
    typeInto('git-email-input', 'jane@example.com');
    fireEvent.click(screen.getByTestId('git-identity-save'));

    expect(await screen.findByRole('alert')).toHaveTextContent('git email is not a valid address');
    expect(screen.getByTestId('git-identity-save')).toBeEnabled();
  });
});
