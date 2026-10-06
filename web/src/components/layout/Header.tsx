import React from 'react';
import { UserNav } from '@/features/auth/components/UserNav';
import { TeamSwitcher } from '@/features/teams/components/TeamSwitcher';
import { ThemeSettingsDialog } from '@/components/theme/ThemeSettingsDialog';

export const Header: React.FC = () => {
  return (
    <header className="border-b border-border bg-card/50 backdrop-blur px-6 py-3 flex items-center justify-between">
      <div className="flex items-center gap-6">
        <div className="flex items-center gap-3">
          <div className="size-8 rounded-lg bg-primary text-primary-foreground flex items-center justify-center font-bold shadow-sm">
            ATM
          </div>
          <div>
            <h1 className="font-semibold text-sm leading-tight tracking-tight">Agent Task Manager</h1>
            <p className="text-xs text-muted-foreground">Self-Hostable Autonomous Control Plane</p>
          </div>
        </div>

        <TeamSwitcher />
      </div>

      <div className="flex items-center gap-3">
        <ThemeSettingsDialog />
        <UserNav />
      </div>
    </header>
  );
};
