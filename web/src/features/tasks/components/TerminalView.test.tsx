import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { TerminalView } from './TerminalView';
import { ThemeProvider } from '@/components/theme/ThemeProvider';

describe('TerminalView', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders terminal session toolbar with connection badge and action buttons', () => {
    render(
      <ThemeProvider>
        <TerminalView taskId="test-task-123" />
      </ThemeProvider>
    );

    expect(screen.getByText('terminal_session')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /copy/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /clear/i })).toBeInTheDocument();
    expect(screen.getByTestId('terminal-container')).toBeInTheDocument();
  });
});
