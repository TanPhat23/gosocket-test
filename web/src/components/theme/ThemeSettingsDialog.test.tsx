import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ThemeProvider } from './ThemeProvider';
import { ThemeSettingsDialog } from './ThemeSettingsDialog';

describe('ThemeSettingsDialog', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('shows only the trigger until opened, then loads the settings body', async () => {
    render(
      <ThemeProvider>
        <ThemeSettingsDialog />
      </ThemeProvider>
    );

    const trigger = screen.getByTitle('Appearance & Theme Settings');
    expect(screen.queryByText('Application Theme')).not.toBeInTheDocument();

    fireEvent.click(trigger);

    expect(await screen.findByText('Application Theme')).toBeInTheDocument();
    expect(screen.getByText('Terminal Logs Theme')).toBeInTheDocument();
  });
});
