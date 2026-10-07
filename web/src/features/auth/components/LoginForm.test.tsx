import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { LoginForm } from './LoginForm';
import { useAuthStore } from '../store/useAuthStore';

describe('LoginForm', () => {
  const loginMock = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.setState({
      user: null,
      isLoading: false,
      isInitialized: true,
      error: null,
      login: loginMock,
    });
  });

  it('renders title, subtitle, pre-filled inputs, submit button, and SSO link', () => {
    render(<LoginForm />);

    expect(screen.getByText('Agent Task Manager')).toBeInTheDocument();
    expect(screen.getByText('Sign in to access your control plane')).toBeInTheDocument();

    const emailInput = screen.getByLabelText('Email') as HTMLInputElement;
    const passwordInput = screen.getByLabelText('Password') as HTMLInputElement;

    expect(emailInput).toBeInTheDocument();
    expect(emailInput.value).toBe('admin@example.com');

    expect(passwordInput).toBeInTheDocument();
    expect(passwordInput.value).toBe('password123');

    const submitBtn = screen.getByRole('button', { name: 'Sign In' });
    expect(submitBtn).toBeInTheDocument();
    expect(submitBtn).not.toBeDisabled();

    // Base UI Button with render={<a />} renders an anchor with role="button" unless role is explicitly overridden
    const ssoLink = screen.getByRole('button', { name: /Sign in with SSO/i });
    expect(ssoLink).toBeInTheDocument();
    expect(ssoLink).toHaveAttribute('href', '/api/v1/auth/login');

    expect(screen.getByText(/Local demo:/i)).toBeInTheDocument();
    expect(screen.getByText('admin@example.com')).toBeInTheDocument();
    expect(screen.getByText('password123')).toBeInTheDocument();
  });

  it('submits form with entered credentials when submitted', async () => {
    loginMock.mockResolvedValueOnce(undefined);
    render(<LoginForm />);

    const emailInput = screen.getByLabelText('Email');
    const passwordInput = screen.getByLabelText('Password');

    fireEvent.change(emailInput, { target: { value: 'custom@example.com' } });
    fireEvent.change(passwordInput, { target: { value: 'secretpass' } });

    const submitBtn = screen.getByRole('button', { name: 'Sign In' });
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(loginMock).toHaveBeenCalledWith({
        email: 'custom@example.com',
        password: 'secretpass',
      });
    });
  });

  it('displays error banner if error exists in auth store', () => {
    useAuthStore.setState({
      error: 'Invalid credentials provided',
    });

    render(<LoginForm />);

    const alertBanner = screen.getByRole('alert');
    expect(alertBanner).toBeInTheDocument();
    expect(alertBanner).toHaveTextContent('Invalid credentials provided');
  });

  it('disables inputs and button with "Signing in..." state while loading', () => {
    useAuthStore.setState({
      isLoading: true,
    });

    render(<LoginForm />);

    const submitBtn = screen.getByRole('button', { name: 'Signing in...' });
    expect(submitBtn).toBeDisabled();

    const emailInput = screen.getByLabelText('Email');
    const passwordInput = screen.getByLabelText('Password');
    expect(emailInput).toBeDisabled();
    expect(passwordInput).toBeDisabled();
  });
});
