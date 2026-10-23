import React, { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { authApi } from '../api/authApi';
import { useAuthStore, useUser } from '../store/useAuthStore';

// Mirrors the server's rule: a name and an email together, or neither.
function validate(name: string, email: string): string | null {
  if (!name && !email) return null;
  if (!name || !email) return 'Enter both a name and an email, or clear both.';
  if (/[<>]/.test(name)) return 'The name cannot contain < or >.';
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) return 'Enter a valid email address.';
  return null;
}

// The author name and email the user's task commits carry, saved to their account.
export const GitIdentitySetting: React.FC = () => {
  const user = useUser();
  const queryClient = useQueryClient();
  const savedName = user?.git_name ?? '';
  const savedEmail = user?.git_email ?? '';
  const [name, setName] = useState(savedName);
  const [email, setEmail] = useState(savedEmail);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const trimmedName = name.trim();
  const trimmedEmail = email.trim();
  const invalid = validate(trimmedName, trimmedEmail);
  const dirty = trimmedName !== savedName || trimmedEmail !== savedEmail;
  const problem = dirty ? invalid : null;

  const save = async () => {
    setSaving(true);
    setError(null);
    setSaved(false);
    try {
      const updated = await authApi.setGitIdentity({ git_name: trimmedName, git_email: trimmedEmail });
      useAuthStore.getState().setUser(updated);
      queryClient.setQueryData(['auth', 'user'], updated);
      setName(updated.git_name ?? '');
      setEmail(updated.git_email ?? '');
      setSaved(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not save the git identity.');
    } finally {
      setSaving(false);
    }
  };

  return (
    <form
      className="space-y-2"
      onSubmit={(e) => {
        e.preventDefault();
        if (dirty && !invalid && !saving) void save();
      }}
    >
      <div className="grid gap-2 sm:grid-cols-2">
        <Input
          value={name}
          onChange={(e) => {
            setName(e.target.value);
            setSaved(false);
          }}
          placeholder="Name"
          autoComplete="name"
          maxLength={100}
          aria-label="Git name"
          data-testid="git-name-input"
          className="bg-card text-xs"
        />
        <Input
          type="email"
          value={email}
          onChange={(e) => {
            setEmail(e.target.value);
            setSaved(false);
          }}
          placeholder="you@example.com"
          autoComplete="email"
          maxLength={254}
          aria-label="Git email"
          data-testid="git-email-input"
          className="bg-card font-mono text-xs"
        />
      </div>
      <div className="flex items-center justify-between gap-2">
        <p
          role={error ? 'alert' : 'status'}
          className={`min-h-4 text-[11px] ${error || problem ? 'text-destructive' : 'text-muted-foreground'}`}
        >
          {error ?? problem ?? (saved ? 'Saved.' : '')}
        </p>
        <Button type="submit" size="sm" disabled={!dirty || !!invalid || saving} className="h-7 px-3 text-xs" data-testid="git-identity-save">
          {saving ? 'Saving…' : 'Save'}
        </Button>
      </div>
    </form>
  );
};
