import React from 'react';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card';
import { Activity, Database, Cpu, ShieldCheck } from 'lucide-react';
import { useUser } from '@/features/auth/store/useAuthStore';

export const TaskMetricsGrid: React.FC = () => {
  const user = useUser();

  return (
    <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
      <Card>
        <CardHeader className="flex flex-row items-center justify-between pb-2">
          <CardTitle className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
            Control Plane
          </CardTitle>
          <Activity className="size-4 text-emerald-500" />
        </CardHeader>
        <CardContent>
          <div className="text-xl font-bold text-foreground">Online</div>
          <p className="text-xs text-muted-foreground mt-1">Fiber v3 REST &amp; WebSocket Gateway</p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between pb-2">
          <CardTitle className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
            Database Tier
          </CardTitle>
          <Database className="size-4 text-primary" />
        </CardHeader>
        <CardContent>
          <div className="text-xl font-bold text-foreground">PostgreSQL 18</div>
          <p className="text-xs text-muted-foreground mt-1">15s statement timeout enforced</p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between pb-2">
          <CardTitle className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
            Task Queue
          </CardTitle>
          <Cpu className="size-4 text-amber-500" />
        </CardHeader>
        <CardContent>
          <div className="text-xl font-bold text-foreground">Asynq Redis 7</div>
          <p className="text-xs text-muted-foreground mt-1">3 priority tiers (6/3/1 weights)</p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between pb-2">
          <CardTitle className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
            Authentication
          </CardTitle>
          <ShieldCheck className="size-4 text-emerald-500" />
        </CardHeader>
        <CardContent>
          <div className="text-xl font-bold text-foreground">Dual Auth</div>
          <p className="text-xs text-muted-foreground mt-1">
            {user ? 'OIDC Session Verified' : 'Session + PAT Bearer'}
          </p>
        </CardContent>
      </Card>
    </div>
  );
};
