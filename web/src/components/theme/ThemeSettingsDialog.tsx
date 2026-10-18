import React, { Suspense, lazy, useState } from 'react';
import { useTheme } from './ThemeProvider';
import { Dialog, DialogTrigger } from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Sun, Moon, Palette } from 'lucide-react';

// The settings body loads the first time the dialog opens. The header trigger stays in the page bundle.
const ThemeSettingsDialogContent = lazy(() =>
  import('./ThemeSettingsDialogContent').then((m) => ({ default: m.ThemeSettingsDialogContent }))
);

export const ThemeSettingsDialog: React.FC = () => {
  const [open, setOpen] = useState(false);
  // Kept true after the first open so the content stays mounted through its close animation.
  const [hasOpened, setHasOpened] = useState(false);
  const { theme, resolvedTheme } = useTheme();

  const handleOpenChange = (next: boolean) => {
    if (next) setHasOpened(true);
    setOpen(next);
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger
        render={
          <Button
            variant="outline"
            size="sm"
            className="h-8 gap-2 px-2.5 text-xs text-muted-foreground hover:text-foreground"
            title="Appearance & Theme Settings"
          />
        }
      >
        {resolvedTheme === 'dark' ? (
          <Moon className="size-3.5 text-sky-400" />
        ) : (
          <Sun className="size-3.5 text-amber-500" />
        )}
        <span className="capitalize hidden sm:inline">{theme}</span>
        <Palette className="size-3 text-muted-foreground ml-0.5" />
      </DialogTrigger>

      {hasOpened && (
        <Suspense fallback={null}>
          <ThemeSettingsDialogContent />
        </Suspense>
      )}
    </Dialog>
  );
};
