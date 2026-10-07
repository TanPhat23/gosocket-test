import React, { useState, useEffect, useCallback } from 'react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogTrigger,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import {
  useActiveProjectId,
  useProjectsList,
  useActiveTeamId,
} from '@/features/teams/store/useTeamStore';
import { integrationApi } from '../api/integrationApi';
import { credentialApi } from '@/features/teams/api/credentialApi';
import type { IntegrationDTO } from '../schemas/integrationSchemas';
import type { CredentialDTO } from '@/features/teams/schemas/credentialSchemas';
import {
  GitBranch,
  Plus,
  Trash2,
  Copy,
  Check,
  ExternalLink,
  KeyRound,
  AlertCircle,
  RefreshCw,
  SlidersHorizontal,
} from 'lucide-react';

export interface ProjectIntegrationModalProps {
  trigger?: React.ReactNode;
}

export const ProjectIntegrationModal: React.FC<ProjectIntegrationModalProps> = ({
  trigger,
}) => {
  const [open, setOpen] = useState(false);
  const activeProjectId = useActiveProjectId();
  const activeTeamId = useActiveTeamId();
  const projects = useProjectsList();

  const currentProject = projects.find((p) => p.id === activeProjectId);

  const [integrations, setIntegrations] = useState<IntegrationDTO[]>([]);
  const [credentials, setCredentials] = useState<CredentialDTO[]>([]);
  const [loading, setLoading] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  // Form states
  const [showAddForm, setShowAddForm] = useState(false);
  const [repo, setRepo] = useState('');
  const [credentialId, setCredentialId] = useState('');
  const [baseBranch, setBaseBranch] = useState('main');
  const [autoPr, setAutoPr] = useState(true);
  const [filterLabel, setFilterLabel] = useState('');
  const [webhookSecret, setWebhookSecret] = useState('');

  // New credential subform state
  const [isCreatingNewCred, setIsCreatingNewCred] = useState(false);
  const [newCredLabel, setNewCredLabel] = useState('');
  const [newCredSecret, setNewCredSecret] = useState('');
  const [credCreating, setCredCreating] = useState(false);

  const [formErrors, setFormErrors] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState(false);
  const [copiedWebhook, setCopiedWebhook] = useState(false);
  const [copiedSecret, setCopiedSecret] = useState(false);

  const webhookUrl =
    typeof window !== 'undefined'
      ? `${window.location.protocol}//${window.location.host}/api/v1/webhooks/github`
      : 'http://localhost:8080/api/v1/webhooks/github';

  const fetchData = useCallback(async () => {
    if (!activeProjectId) return;
    setLoading(true);
    setActionError(null);
    try {
      const [integs, creds] = await Promise.all([
        integrationApi.listProjectIntegrations(activeProjectId),
        activeTeamId ? credentialApi.listCredentials(activeTeamId) : Promise.resolve([]),
      ]);
      setIntegrations(integs);
      const ghCreds = creds.filter((c) => c.provider === 'github');
      setCredentials(ghCreds);
      setCredentialId((prev) => (prev ? prev : ghCreds.length > 0 ? ghCreds[0].id : ''));
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Failed to load integrations');
    } finally {
      setLoading(false);
    }
  }, [activeProjectId, activeTeamId]);

  useEffect(() => {
    if (open && activeProjectId) {
      fetchData();
    }
  }, [open, activeProjectId, fetchData]);

  const handleCreateCredential = async () => {
    if (!activeTeamId) {
      setActionError('Active team required to create credential');
      return;
    }
    if (!newCredLabel.trim()) {
      setFormErrors((prev) => ({ ...prev, cred_label: 'Label is required' }));
      return;
    }
    if (!newCredSecret.trim()) {
      setFormErrors((prev) => ({ ...prev, cred_secret: 'Token is required' }));
      return;
    }

    setCredCreating(true);
    try {
      const created = await credentialApi.createCredential(activeTeamId, {
        provider: 'github',
        label: newCredLabel.trim(),
        secret: newCredSecret.trim(),
      });
      setCredentials((prev) => [...prev, created]);
      setCredentialId(created.id);
      setIsCreatingNewCred(false);
      setNewCredLabel('');
      setNewCredSecret('');
      setFormErrors((prev) => {
        const next = { ...prev };
        delete next.cred_label;
        delete next.cred_secret;
        return next;
      });
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Failed to save credential');
    } finally {
      setCredCreating(false);
    }
  };

  const handleCopyWebhook = () => {
    navigator.clipboard.writeText(webhookUrl);
    setCopiedWebhook(true);
    setTimeout(() => setCopiedWebhook(false), 2000);
  };

  const handleCopySecret = () => {
    if (!webhookSecret) return;
    navigator.clipboard.writeText(webhookSecret);
    setCopiedSecret(true);
    setTimeout(() => setCopiedSecret(false), 2000);
  };

  const handleAddIntegration = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!activeProjectId) return;

    const errors: Record<string, string> = {};
    const repoTrim = repo.trim();
    if (!repoTrim) {
      errors.repo = 'Repository is required';
    } else if (!/^[^/\s]+\/[^/\s]+$/.test(repoTrim)) {
      errors.repo = 'Format must be owner/repo (e.g. facebook/react)';
    }

    if (!credentialId) {
      errors.credential_id = 'GitHub credential / PAT is required';
    }

    if (Object.keys(errors).length > 0) {
      setFormErrors(errors);
      return;
    }

    setFormErrors({});
    setSubmitting(true);
    setActionError(null);

    try {
      const created = await integrationApi.createProjectIntegration(activeProjectId, {
        credential_id: credentialId,
        provider: 'github',
        repo: repoTrim,
        base_branch: baseBranch.trim() || 'main',
        auto_pr: autoPr,
        webhook_secret: webhookSecret.trim() || undefined,
        filter_label: filterLabel.trim() || undefined,
      });

      setIntegrations((prev) => [...prev, created]);
      setRepo('');
      setWebhookSecret('');
      setFilterLabel('');
      setShowAddForm(false);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Failed to connect repository');
    } finally {
      setSubmitting(false);
    }
  };

  const handleDeleteIntegration = async (id: string) => {
    if (!activeProjectId) return;
    if (!confirm('Are you sure you want to remove this GitHub integration?')) return;

    try {
      await integrationApi.deleteProjectIntegration(activeProjectId, id);
      setIntegrations((prev) => prev.filter((item) => item.id !== id));
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Failed to delete integration');
    }
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger
        render={
          trigger ? (
            (trigger as React.ReactElement)
          ) : (
            <Button
              variant="outline"
              size="sm"
              className="gap-1.5 h-8 text-xs font-medium"
              title="Manage Project Integrations"
              data-testid="integrations-trigger-button"
            >
              <GitBranch className="size-3.5" />
              <span>Integrations</span>
            </Button>
          )
        }
      />

      <DialogContent
        className="sm:max-w-2xl max-h-[90vh] overflow-y-auto p-6"
        data-testid="project-integration-modal"
      >
        <DialogHeader className="pb-3 border-b border-border">
          <div className="flex items-center justify-between gap-3 pr-6">
            <div className="flex items-center gap-2">
              <div className="size-8 rounded-md bg-muted flex items-center justify-center text-foreground">
                <GitBranch className="size-4" />
              </div>
              <div>
                <DialogTitle className="text-base font-semibold leading-none">
                  Project Integrations
                </DialogTitle>
                <DialogDescription className="text-xs text-muted-foreground mt-1">
                  Connect GitHub repositories to sync issues and trigger autonomous tasks.
                </DialogDescription>
              </div>
            </div>
            {currentProject && (
              <Badge variant="outline" className="font-mono text-xs">
                {currentProject.name}
              </Badge>
            )}
          </div>
        </DialogHeader>

        {actionError && (
          <div className="flex items-start gap-2 p-3 text-xs bg-destructive/10 border border-destructive/20 rounded-md text-destructive">
            <AlertCircle className="size-4 shrink-0 mt-0.5" />
            <div className="flex-1 font-medium">{actionError}</div>
          </div>
        )}

        {/* Existing integrations list */}
        <div className="space-y-3 pt-2">
          <div className="flex items-center justify-between">
            <h4 className="text-xs font-semibold text-foreground uppercase tracking-wider flex items-center gap-1.5">
              <GitBranch className="size-3.5 text-primary" /> Connected Repositories
            </h4>
            <Button
              size="xs"
              variant={showAddForm ? 'secondary' : 'default'}
              onClick={() => setShowAddForm(!showAddForm)}
              className="h-7 text-xs gap-1"
              data-testid="toggle-connect-repo-btn"
            >
              <Plus className="size-3" />
              {showAddForm ? 'Close Form' : 'Connect GitHub Repository'}
            </Button>
          </div>

          {loading ? (
            <div className="p-8 text-center text-xs text-muted-foreground">
              <RefreshCw className="size-4 animate-spin mx-auto mb-2 text-muted-foreground" />
              Loading integrations...
            </div>
          ) : integrations.length === 0 ? (
            <div className="p-6 rounded-lg border border-dashed border-border/80 bg-muted/20 text-center text-xs text-muted-foreground space-y-1">
              <GitBranch className="size-6 mx-auto mb-1 opacity-40" />
              <p className="font-medium text-foreground">No GitHub repositories connected</p>
              <p>Click "Connect GitHub Repository" to link a repo to this project.</p>
            </div>
          ) : (
            <div className="space-y-2">
              {integrations.map((item) => (
                <div
                  key={item.id}
                  className="flex items-center justify-between gap-3 p-3 rounded-lg border border-border bg-card shadow-xs"
                  data-testid={`integration-row-${item.id}`}
                >
                  <div className="flex items-center gap-2.5 min-w-0">
                    <GitBranch className="size-4 text-muted-foreground shrink-0" />
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-xs text-foreground font-mono truncate">
                          {item.external_id}
                        </span>
                        {item.enabled ? (
                          <Badge className="bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border border-emerald-500/30 text-[10px] font-mono py-0 h-4.5">
                            Active
                          </Badge>
                        ) : (
                          <Badge variant="outline" className="text-[10px] font-mono py-0 h-4.5">
                            Disabled
                          </Badge>
                        )}
                      </div>
                      {item.external_url && (
                        <a
                          href={item.external_url}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="text-[11px] text-muted-foreground hover:text-primary flex items-center gap-1 mt-0.5 truncate"
                        >
                          <span className="truncate">{item.external_url}</span>
                          <ExternalLink className="size-2.5 shrink-0" />
                        </a>
                      )}
                    </div>
                  </div>

                  <div className="flex items-center gap-2 shrink-0">
                    <Button
                      size="icon-xs"
                      variant="ghost"
                      onClick={() => handleDeleteIntegration(item.id)}
                      className="text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                      title="Disconnect repository"
                      data-testid={`delete-integration-${item.id}`}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Connect Form */}
        {showAddForm && (
          <form
            onSubmit={handleAddIntegration}
            className="p-4 rounded-lg border border-primary/30 bg-muted/20 space-y-4 animate-in fade-in-50 duration-150"
            data-testid="connect-repo-form"
          >
            <div className="flex items-center gap-2 pb-2 border-b border-border/60">
              <SlidersHorizontal className="size-4 text-primary" />
              <h4 className="text-xs font-semibold text-foreground">Connect Repository</h4>
            </div>

            {/* Repo input */}
            <div className="space-y-1">
              <label htmlFor="repo-input" className="text-xs font-medium text-foreground">
                Repository (owner/repo) *
              </label>
              <Input
                id="repo-input"
                placeholder="e.g. facebook/react or octocat/hello-world"
                value={repo}
                onChange={(e) => setRepo(e.target.value)}
                className="font-mono text-xs"
                data-testid="repo-input"
              />
              {formErrors.repo && (
                <p className="text-[11px] text-destructive font-medium">{formErrors.repo}</p>
              )}
            </div>

            {/* Credential selection */}
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <label className="text-xs font-medium text-foreground flex items-center gap-1.5">
                  <KeyRound className="size-3 text-muted-foreground" /> GitHub Credential (PAT) *
                </label>
                <Button
                  type="button"
                  variant="ghost"
                  size="xs"
                  onClick={() => setIsCreatingNewCred(!isCreatingNewCred)}
                  className="h-6 text-[11px] text-primary hover:underline p-0"
                >
                  {isCreatingNewCred ? 'Select Existing' : '+ Add New PAT'}
                </Button>
              </div>

              {!isCreatingNewCred ? (
                <div className="space-y-1">
                  {credentials.length === 0 ? (
                    <div className="p-2.5 rounded border border-border bg-card text-xs text-muted-foreground flex items-center justify-between">
                      <span>No GitHub PAT configured for this team.</span>
                      <Button
                        type="button"
                        size="xs"
                        variant="outline"
                        onClick={() => setIsCreatingNewCred(true)}
                        className="h-6 text-xs"
                      >
                        Create PAT
                      </Button>
                    </div>
                  ) : (
                    <select
                      className="w-full h-8 text-xs font-mono rounded-md border border-input bg-background px-2.5 outline-none focus:border-ring focus:ring-1 focus:ring-ring"
                      value={credentialId}
                      onChange={(e) => setCredentialId(e.target.value)}
                      data-testid="credential-select"
                    >
                      {credentials.map((c) => (
                        <option key={c.id} value={c.id}>
                          {c.label} ({c.id.slice(0, 8)})
                        </option>
                      ))}
                    </select>
                  )}
                  {formErrors.credential_id && (
                    <p className="text-[11px] text-destructive font-medium">
                      {formErrors.credential_id}
                    </p>
                  )}
                </div>
              ) : (
                <div className="p-3 rounded border border-border bg-card space-y-2.5">
                  <div className="text-xs font-medium text-foreground">New Personal Access Token</div>
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                    <div className="space-y-1">
                      <label className="text-[11px] text-muted-foreground">Label</label>
                      <Input
                        placeholder="e.g. Org GitHub Bot"
                        value={newCredLabel}
                        onChange={(e) => setNewCredLabel(e.target.value)}
                        className="text-xs h-7"
                        data-testid="new-cred-label"
                      />
                      {formErrors.cred_label && (
                        <p className="text-[10px] text-destructive">{formErrors.cred_label}</p>
                      )}
                    </div>
                    <div className="space-y-1">
                      <label className="text-[11px] text-muted-foreground">PAT Token / Secret</label>
                      <Input
                        type="password"
                        placeholder="ghp_..."
                        value={newCredSecret}
                        onChange={(e) => setNewCredSecret(e.target.value)}
                        className="text-xs h-7 font-mono"
                        data-testid="new-cred-secret"
                      />
                      {formErrors.cred_secret && (
                        <p className="text-[10px] text-destructive">{formErrors.cred_secret}</p>
                      )}
                    </div>
                  </div>
                  <div className="flex justify-end gap-2 pt-1">
                    <Button
                      type="button"
                      variant="ghost"
                      size="xs"
                      onClick={() => setIsCreatingNewCred(false)}
                      className="h-7 text-xs"
                    >
                      Cancel
                    </Button>
                    <Button
                      type="button"
                      size="xs"
                      onClick={handleCreateCredential}
                      disabled={credCreating}
                      className="h-7 text-xs"
                      data-testid="save-cred-btn"
                    >
                      {credCreating ? 'Saving...' : 'Save Credential'}
                    </Button>
                  </div>
                </div>
              )}
            </div>

            {/* Webhook setup instructions */}
            <div className="p-3 rounded-lg border border-border bg-card space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                  GitHub Webhook Setup
                </span>
                <span className="text-[11px] text-muted-foreground font-mono">
                  Payload URL &amp; Secret
                </span>
              </div>
              <p className="text-[11px] text-muted-foreground leading-relaxed">
                Add this URL in GitHub Repo &gt; <b>Settings &gt; Webhooks &gt; Add webhook</b>. Select Content type <code className="font-mono text-foreground">application/json</code> and trigger on <code className="font-mono text-foreground">Issues</code>.
              </p>

              <div className="space-y-1.5 pt-1">
                <label className="text-[11px] font-medium text-foreground">Payload URL</label>
                <div className="flex items-center gap-1.5">
                  <Input
                    readOnly
                    value={webhookUrl}
                    className="font-mono text-[11px] h-7 bg-muted/50 select-all"
                    data-testid="webhook-url-input"
                  />
                  <Button
                    type="button"
                    size="icon-xs"
                    variant="outline"
                    onClick={handleCopyWebhook}
                    className="h-7 w-7 shrink-0"
                    title="Copy Payload URL"
                    data-testid="copy-webhook-btn"
                  >
                    {copiedWebhook ? (
                      <Check className="size-3 text-emerald-500" />
                    ) : (
                      <Copy className="size-3" />
                    )}
                  </Button>
                </div>
              </div>

              <div className="space-y-1.5 pt-1">
                <div className="flex items-center justify-between">
                  <label htmlFor="webhook-secret-input" className="text-[11px] font-medium text-foreground">
                    Webhook Secret (Optional)
                  </label>
                  {webhookSecret && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="xs"
                      onClick={handleCopySecret}
                      className="h-5 text-[10px] text-muted-foreground gap-1 px-1"
                    >
                      {copiedSecret ? <Check className="size-2.5 text-emerald-500" /> : <Copy className="size-2.5" />}
                      Copy
                    </Button>
                  )}
                </div>
                <Input
                  id="webhook-secret-input"
                  placeholder="Enter secret configured in GitHub repo webhook"
                  value={webhookSecret}
                  onChange={(e) => setWebhookSecret(e.target.value)}
                  className="font-mono text-[11px] h-7"
                  data-testid="webhook-secret-input"
                />
              </div>
            </div>

            {/* Settings */}
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 pt-1">
              <div className="space-y-1">
                <label htmlFor="base-branch-input" className="text-xs font-medium text-foreground">
                  Base Branch
                </label>
                <Input
                  id="base-branch-input"
                  placeholder="main"
                  value={baseBranch}
                  onChange={(e) => setBaseBranch(e.target.value)}
                  className="text-xs h-8 font-mono"
                  data-testid="base-branch-input"
                />
              </div>

              <div className="space-y-1">
                <label htmlFor="filter-label-input" className="text-xs font-medium text-foreground">
                  Filter Label (Optional)
                </label>
                <Input
                  id="filter-label-input"
                  placeholder="e.g. agent or auto-task"
                  value={filterLabel}
                  onChange={(e) => setFilterLabel(e.target.value)}
                  className="text-xs h-8 font-mono"
                  data-testid="filter-label-input"
                />
              </div>
            </div>

            {/* Auto PR Checkbox */}
            <div className="flex items-center gap-2 pt-1">
              <input
                type="checkbox"
                id="auto-pr-checkbox"
                checked={autoPr}
                onChange={(e) => setAutoPr(e.target.checked)}
                className="size-4 rounded border-input text-primary focus:ring-ring"
                data-testid="auto-pr-checkbox"
              />
              <label htmlFor="auto-pr-checkbox" className="text-xs text-foreground cursor-pointer">
                Auto-create Pull Request on completion
              </label>
            </div>

            {/* Actions */}
            <div className="flex justify-end gap-2 pt-2 border-t border-border/40">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => setShowAddForm(false)}
                className="text-xs"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                size="sm"
                disabled={submitting}
                className="text-xs font-medium"
                data-testid="connect-submit-btn"
              >
                {submitting ? 'Connecting...' : 'Connect Repository'}
              </Button>
            </div>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
};
