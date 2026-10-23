import React, { Suspense, lazy, useState } from 'react';
import { Dialog, DialogTrigger } from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { GitBranch } from 'lucide-react';

// The dialog body loads the first time the dialog opens. Only the trigger stays in the page bundle.
const ProjectIntegrationModalContent = lazy(() =>
  import('./ProjectIntegrationModalContent').then((m) => ({ default: m.ProjectIntegrationModalContent }))
);

export interface ProjectIntegrationModalProps {
  trigger?: React.ReactNode;
}

export const ProjectIntegrationModal: React.FC<ProjectIntegrationModalProps> = ({ trigger }) => {
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
              className="gap-1.5 h-8 text-xs font-medium cursor-pointer"
              title="Manage Project Integrations"
              data-testid="integrations-trigger-button"
            >
              <GitBranch className="size-3.5" />
              <span>Integrations</span>
            </Button>
          )
        }
      />

      {hasOpened && (
        <Suspense fallback={null}>
          <ProjectIntegrationModalContent open={open} />
        </Suspense>
      )}
    </Dialog>
  );
};
