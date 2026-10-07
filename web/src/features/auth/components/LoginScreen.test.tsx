import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { LoginScreen } from './LoginScreen';

describe('LoginScreen', () => {
  it('renders branding title, subtitle, and agent status', () => {
    render(<LoginScreen />);

    expect(
      screen.getByRole('heading', { level: 1, name: /Autonomous Coding Control Plane/i })
    ).toBeInTheDocument();

    expect(
      screen.getByText(
        /Orchestrate agents across Git worktrees, OpenCode sessions, and automated GitHub pull requests\./i
      )
    ).toBeInTheDocument();

    expect(screen.getByText(/ATM Core Security/i)).toBeInTheDocument();
  });

  it('renders prominent SSO sign in button pointing to /api/v1/auth/login', () => {
    render(<LoginScreen />);

    const ssoLink = screen.getByRole('link', { name: /Sign in with SSO/i });
    expect(ssoLink).toBeInTheDocument();
    expect(ssoLink).toHaveAttribute('href', '/api/v1/auth/login');
  });

  it('renders credentials hint card for local development with admin and developer accounts', () => {
    render(<LoginScreen />);

    expect(screen.getByText(/Local Development Credentials/i)).toBeInTheDocument();
    expect(screen.getByText(/admin@example.com/i)).toBeInTheDocument();
    expect(screen.getByText(/developer@example.com/i)).toBeInTheDocument();
    expect(screen.getAllByText('password123')).toHaveLength(2);
  });
});
