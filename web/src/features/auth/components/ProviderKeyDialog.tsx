import React, { Suspense, lazy, useEffect, useState } from 'react';
import { KeyRound } from 'lucide-react';
import { axiosClient } from '@/lib/axios';
import { Button } from '@/components/ui/button';
import { Dialog, DialogTrigger } from '@/components/ui/dialog';

// The form loads the first time the dialog opens. Only the trigger stays in the header bundle.
const ProviderKeyDialogContent = lazy(() =>
  import('./ProviderKeyDialogContent').then((m) => ({ default: m.ProviderKeyDialogContent }))
);

export interface ProviderKeyStatus {
  operator: boolean;
  writable?: boolean;
  configured?: boolean;
  updated_at?: string;
}

/**
 * Operator-only control for the LLM gateway provider key. It renders nothing for anyone else. The key is
 * write-only: the server never returns it, and it is cleared from the form as soon as it is sent.
 */
export const ProviderKeyDialog: React.FC = () => {
  const [status, setStatus] = useState<ProviderKeyStatus | null>(null);
  const [open, setOpen] = useState(false);
  // Kept true after the first open so the content stays mounted through its close animation.
  const [hasOpened, setHasOpened] = useState(false);
  const [key, setKey] = useState('');
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);

  const load = async () => {
    try {
      const res = await axiosClient.get<ProviderKeyStatus>('/operator/provider-key');
      setStatus(res.data);
    } catch {
      setStatus(null);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  if (!status?.operator) return null;

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!key.trim() || busy) return;
    setBusy(true);
    setMessage(null);
    try {
      await axiosClient.put('/operator/provider-key', { key });
      setKey('');
      setMessage('Provider key saved. New requests use it immediately.');
      await load();
    } catch (err: unknown) {
      const apiErr = err as { response?: { data?: { error?: string } }; message?: string };
      setKey('');
      setMessage(apiErr.response?.data?.error || apiErr.message || 'Failed to save the key.');
    } finally {
      setBusy(false);
    }
  };

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
            title="LLM provider key (operator)"
            data-testid="provider-key-btn"
          />
        }
      >
        <KeyRound className="size-3.5" />
        <span className="hidden sm:inline">Provider key</span>
      </DialogTrigger>
      {hasOpened && (
        <Suspense fallback={null}>
          <ProviderKeyDialogContent
            status={status}
            keyValue={key}
            busy={busy}
            message={message}
            onKeyChange={setKey}
            onSubmit={save}
          />
        </Suspense>
      )}
    </Dialog>
  );
};
