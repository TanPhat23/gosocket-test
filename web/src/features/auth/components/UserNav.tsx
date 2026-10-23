import React from 'react';
import { useUser, useAuthLoading, useAuthStore } from '../store/useAuthStore';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { LogOut, LogIn } from 'lucide-react';

export const UserNav: React.FC = () => {
  const user = useUser();
  const loading = useAuthLoading();
  const logout = useAuthStore((state) => state.logout);

  if (loading) {
    return (
      <Badge variant="outline" className="text-xs text-muted-foreground">
        Checking session...
      </Badge>
    );
  }

  if (user) {
    return (
      <div className="flex items-center gap-3">
        <div className="text-right">
          <div className="text-xs font-medium text-foreground">
            {user.display_name || user.email || 'Authenticated User'}
          </div>
          <div className="flex items-center gap-1.5 justify-end">
            <span className="size-1.5 rounded-full bg-emerald-500" />
            <span className="text-[10px] text-muted-foreground">OIDC Active</span>
          </div>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => logout()}
        >
          <LogOut data-icon="inline-start" />
          Sign Out
        </Button>
      </div>
    );
  }

  return (
    <Button
      variant="default"
      size="sm"
      render={<a href="/api/v1/auth/login" />}
    >
      <LogIn data-icon="inline-start" />
      Sign In
    </Button>
  );
};
