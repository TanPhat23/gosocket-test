import React from 'react';
import { useTaskStore } from '../store/useTaskStore';
import { useActiveProjectId } from '@/features/teams/store/useTeamStore';
import { useShallow } from 'zustand/react/shallow';
import { TaskCard } from './TaskCard';
import { Badge } from '@/components/ui/badge';
import { Inbox, Clock, PlayCircle, Eye, CheckCircle2 } from 'lucide-react';

interface ColumnDef {
  id: string;
  title: string;
  icon: React.ComponentType<{ className?: string }>;
  statuses: string[];
}

const COLUMNS: ColumnDef[] = [
  {
    id: 'backlog',
    title: 'Backlog',
    icon: Inbox,
    statuses: ['backlog'],
  },
  {
    id: 'queued',
    title: 'Queued',
    icon: Clock,
    statuses: ['queued'],
  },
  {
    id: 'in_progress',
    title: 'In Progress',
    icon: PlayCircle,
    statuses: ['in_progress'],
  },
  {
    id: 'review',
    title: 'Review',
    icon: Eye,
    statuses: ['review'],
  },
  {
    id: 'done',
    title: 'Done',
    icon: CheckCircle2,
    statuses: ['done', 'failed', 'cancelled'],
  },
];

interface KanbanLaneProps {
  column: ColumnDef;
}

const KanbanLane: React.FC<KanbanLaneProps> = ({ column }) => {
  const activeProjectId = useActiveProjectId();
  // Use shallow selector to subscribe only to IDs belonging to this lane and active project
  const taskIds = useTaskStore(
    useShallow((state) =>
      state.taskIds.filter((id) => {
        const task = state.tasks[id];
        if (!task) return false;
        if (activeProjectId && task.project_id && task.project_id !== activeProjectId) {
          return false;
        }
        return column.statuses.includes(task.status);
      })
    )
  );

  return (
    <div
      className="flex flex-col bg-muted/40 rounded-xl border border-border/60 p-3 min-h-[500px]"
      data-testid={`kanban-lane-${column.id}`}
    >
      {/* Column Header */}
      <div className="flex items-center justify-between pb-3 mb-3 border-b border-border/50 px-1">
        <div className="flex items-center gap-2">
          <column.icon className="size-4 text-muted-foreground" />
          <h2 className="text-xs font-semibold uppercase tracking-wider text-foreground">
            {column.title}
          </h2>
        </div>
        <Badge variant="secondary" className="font-mono text-xs px-2 py-0.5">
          {taskIds.length}
        </Badge>
      </div>

      {/* Task List or Empty Placeholder */}
      <div className="flex-1 flex flex-col gap-2.5 overflow-y-auto">
        {taskIds.length === 0 ? (
          <div className="flex-1 flex flex-col items-center justify-center p-6 text-center border border-dashed border-border/70 rounded-lg bg-background/30 text-muted-foreground">
            <p className="text-xs font-medium">No tasks in this lane</p>
            <p className="text-[11px] text-muted-foreground/70 mt-0.5">
              Tasks in {column.title.toLowerCase()} state will show here
            </p>
          </div>
        ) : (
          taskIds.map((id) => <TaskCard key={id} id={id} />)
        )}
      </div>
    </div>
  );
};

export const KanbanBoard: React.FC = () => {
  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 xl:grid-cols-5 gap-4 w-full">
      {COLUMNS.map((column) => (
        <KanbanLane key={column.id} column={column} />
      ))}
    </div>
  );
};
