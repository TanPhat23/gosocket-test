import React from 'react';
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';

export const TaskBoardPlaceholder: React.FC = () => {
  return (
    <Card className="flex-1 min-h-[360px] flex flex-col justify-center items-center text-center p-8">
      <CardHeader className="max-w-md mx-auto text-center">
        <CardTitle className="text-base font-semibold">Autonomous Task Execution Board</CardTitle>
        <CardDescription className="text-xs text-muted-foreground mt-1">
          Multi-agent sandbox pipelines, worktree isolation, live terminal log streaming, and review diff inspections.
        </CardDescription>
      </CardHeader>
      <CardContent className="mt-4">
        <Badge variant="secondary" className="text-xs px-3 py-1 font-mono">
          Ready for Task 16: Kanban, Terminal &amp; Diff Components
        </Badge>
      </CardContent>
    </Card>
  );
};
