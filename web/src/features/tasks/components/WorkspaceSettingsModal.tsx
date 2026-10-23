import React, { Suspense, lazy, useState } from 'react';
import { Dialog, DialogTrigger } from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { SlidersHorizontal } from 'lucide-react';

// The dialog body loads the first time the dialog opens. Only the trigger stays in the page bundle.
const WorkspaceSettingsModalContent = lazy(() =>
  import('./WorkspaceSettingsModalContent').then((m) => ({ default: m.WorkspaceSettingsModalContent }))
);

export interface WorkspaceSettingsModalProps {
  trigger?: React.ReactNode;
}

export const WorkspaceSettingsModal: React.FC<WorkspaceSettingsModalProps> = ({ trigger }) => {
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
            <Button
              variant="outline"
              size="sm"
              className="h-8 gap-1.5 px-2.5 text-xs text-muted-foreground hover:text-foreground"
              title="Configure workspace defaults"
              data-testid="workspace-settings-trigger"
            >
              <SlidersHorizontal className="size-3.5" />
              <span className="hidden sm:inline">Settings</span>
            </Button>
          )
        }
      />

      {hasOpened && (
        <Suspense fallback={null}>
          <WorkspaceSettingsModalContent onOpenChange={setOpen} />
        </Suspense>
      )}
    </Dialog>
  );
};
