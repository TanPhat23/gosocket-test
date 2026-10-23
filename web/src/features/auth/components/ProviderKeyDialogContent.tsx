import React from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import type { ProviderKeyStatus } from './ProviderKeyDialog';

interface ProviderKeyDialogContentProps {
  status: ProviderKeyStatus;
  keyValue: string;
  busy: boolean;
  message: string | null;
  onKeyChange: (value: string) => void;
  onSubmit: (e: React.FormEvent) => void;
}

// Loaded when the provider key dialog first opens (see ProviderKeyDialog).
export const ProviderKeyDialogContent: React.FC<ProviderKeyDialogContentProps> = ({
  status,
  keyValue,
  busy,
  message,
  onKeyChange,
  onSubmit,
}) => (
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
    <form onSubmit={onSubmit} className="space-y-3">
      <Input
        type="password"
        autoComplete="off"
        value={keyValue}
        onChange={(e) => onKeyChange(e.target.value)}
        placeholder="Paste the new key"
        disabled={busy || status.writable === false}
        data-testid="provider-key-input"
      />
      <Button type="submit" size="sm" disabled={busy || !keyValue.trim() || status.writable === false} data-testid="provider-key-save">
        {busy ? 'Saving...' : 'Save key'}
      </Button>
      {message && (
        <p className="text-xs text-muted-foreground" role="status" data-testid="provider-key-message">
          {message}
        </p>
      )}
    </form>
  </DialogContent>
);
