import React, { useEffect, useState } from 'react';
import { KeyRound } from 'lucide-react';
import { axiosClient } from '@/lib/axios';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog';

interface ProviderKeyStatus {
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

  return (
    <Dialog open={open} onOpenChange={setOpen}>
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
      <DialogContent className="sm:max-w-md p-6">
        <DialogHeader>
          <DialogTitle className="text-base font-semibold">LLM provider key</DialogTitle>
          <DialogDescription className="text-xs">
            Used by the gateway for every team. It is never shown again after saving. Changing the provider URL or model
            still needs the setup script and a restart.
          </DialogDescription>
        </DialogHeader>
        <p className="text-xs text-muted-foreground" data-testid="provider-key-state">
          {status.writable === false
            ? 'This server does not take its key from a file, so it cannot be changed here.'
            : status.configured
              ? `A key is set${status.updated_at ? ` (changed ${new Date(status.updated_at).toLocaleString()})` : ''}.`
              : 'No key is set.'}
        </p>
        <form onSubmit={save} className="space-y-3">
          <Input
            type="password"
            autoComplete="off"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            placeholder="Paste the new key"
            disabled={busy || status.writable === false}
            data-testid="provider-key-input"
          />
          <Button type="submit" size="sm" disabled={busy || !key.trim() || status.writable === false} data-testid="provider-key-save">
            {busy ? 'Saving...' : 'Save key'}
          </Button>
          {message && (
            <p className="text-xs text-muted-foreground" role="status" data-testid="provider-key-message">
              {message}
            </p>
          )}
        </form>
      </DialogContent>
    </Dialog>
  );
};
