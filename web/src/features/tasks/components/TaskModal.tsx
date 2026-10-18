import React, { Suspense, lazy, useState } from 'react';
import { Dialog, DialogTrigger } from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Plus } from 'lucide-react';

export { DEFAULT_OPENCODE_MODELS, type ModelOption } from '../store/useWorkspaceSettingsStore';

// The dialog body loads the first time the dialog opens. Only the trigger stays in the page bundle.
const TaskModalContent = lazy(() =>
  import('./TaskModalContent').then((m) => ({ default: m.TaskModalContent }))
);

export interface TaskModalProps {
  trigger?: React.ReactNode;
}

export const TaskModal: React.FC<TaskModalProps> = ({ trigger }) => {
  const [open, setOpen] = useState(false);
  // Kept true after the first open so the content stays mounted through its close animation.
  const [hasOpened, setHasOpened] = useState(false);
  const handleOpenChange = (next: boolean) => {
    if (next) setHasOpened(true);
    setOpen(next);
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger
        render={
          trigger ? (
            (trigger as React.ReactElement)
          ) : (
            <Button size="sm" className="gap-1.5 shadow-xs font-medium">
              <Plus className="size-4" />
              New Task
            </Button>
          )
        }
      />

      {hasOpened && (
        <Suspense fallback={null}>
          <TaskModalContent open={open} onOpenChange={setOpen} />
        </Suspense>
      )}
    </Dialog>
  );
};
