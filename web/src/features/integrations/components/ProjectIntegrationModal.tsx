import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { useForm } from '@tanstack/react-form';
import { z } from 'zod';
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
import type { CredentialDTO, GitHubRepoDTO } from '@/features/teams/schemas/credentialSchemas';
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
  Lock,
  Search,
  CheckCircle2,
  ChevronRight,
  Info,
} from 'lucide-react';

export interface ProjectIntegrationModalProps {
  trigger?: React.ReactNode;
}

// Schemas for forms
const patFormSchema = z.object({
  label: z.string().min(1, 'Token label is required').max(100, 'Label cannot exceed 100 characters'),
  secret: z.string().min(1, 'Personal Access Token is required'),
});

const connectRepoSchema = z.object({
  repo: z
    .string()
    .min(1, 'Repository is required')
    .regex(/^[^/\s]+\/[^/\s]+$/, 'Format must be owner/repo (e.g. facebook/react)'),
  credential_id: z.string().min(1, 'GitHub credential / PAT is required'),
  base_branch: z.string().min(1, 'Base branch is required'),
  auto_pr: z.boolean(),
  webhook_secret: z.string(),
  filter_label: z.string(),
});

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

  // Form display toggle
  const [showAddForm, setShowAddForm] = useState(false);

  // Step state for guided flow: 1 = PAT Setup / Verification, 2 = Repo Selection & Configure
  const [activeStep, setActiveStep] = useState<1 | 2>(1);
  const [isCreatingNewCred, setIsCreatingNewCred] = useState(false);

  // GitHub Repos state for Step 2
  const [gitHubRepos, setGitHubRepos] = useState<GitHubRepoDTO[]>([]);
  const [loadingRepos, setLoadingRepos] = useState(false);
  const [repoFilterQuery, setRepoFilterQuery] = useState('');
  const [manualRepoMode, setManualRepoMode] = useState(false);

  const [copiedWebhook, setCopiedWebhook] = useState(false);
  const [copiedSecret, setCopiedSecret] = useState(false);

  const webhookUrl =
    typeof window !== 'undefined'
      ? `${window.location.protocol}//${window.location.host}/api/v1/webhooks/github`
      : 'http://localhost:8080/api/v1/webhooks/github';

  // PAT Form using TanStack Form
  const patForm = useForm({
    defaultValues: {
      label: '',
      secret: '',
    },
    validators: {
      onSubmit: patFormSchema,
    },
    onSubmit: async ({ value }) => {
      if (!activeTeamId) {
        setActionError('Active team required to create credential');
        return;
      }
      setActionError(null);
      try {
        const created = await credentialApi.createCredential(activeTeamId, {
          provider: 'github',
          label: value.label.trim(),
          secret: value.secret.trim(),
        });
        setCredentials((prev) => [...prev, created]);
        connectForm.setFieldValue('credential_id', created.id);
        setIsCreatingNewCred(false);
        patForm.reset();
        // Advance to Step 2
        setActiveStep(2);
        fetchReposForCredential(activeTeamId, created.id);
      } catch (err) {
        setActionError(err instanceof Error ? err.message : 'Failed to save credential');
      }
    },
  });

  // Connect Repo Form using TanStack Form
  const connectForm = useForm({
    defaultValues: {
      repo: '',
      credential_id: '',
      base_branch: 'main',
      auto_pr: true,
      webhook_secret: '',
      filter_label: '',
    },
    validators: {
      onSubmit: connectRepoSchema,
    },
    onSubmit: async ({ value }) => {
      if (!activeProjectId) return;
      setActionError(null);
      try {
        const created = await integrationApi.createProjectIntegration(activeProjectId, {
          credential_id: value.credential_id,
          provider: 'github',
          repo: value.repo.trim(),
          base_branch: value.base_branch.trim() || 'main',
          auto_pr: value.auto_pr,
          webhook_secret: value.webhook_secret?.trim() || undefined,
          filter_label: value.filter_label?.trim() || undefined,
        });

        setIntegrations((prev) => [...prev, created]);
        connectForm.reset();
        setShowAddForm(false);
      } catch (err) {
        setActionError(err instanceof Error ? err.message : 'Failed to connect repository');
      }
    },
  });

  const fetchReposForCredential = useCallback(async (teamId: string, credId: string) => {
    if (!teamId || !credId) return;
    setLoadingRepos(true);
    setActionError(null);
    try {
      const repos = await credentialApi.listGitHubRepositories(teamId, credId);
      setGitHubRepos(repos);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Failed to list GitHub repositories');
    } finally {
      setLoadingRepos(false);
    }
  }, []);

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

      const targetCredId = ghCreds.length > 0 ? ghCreds[0].id : '';
      connectForm.setFieldValue('credential_id', targetCredId);

      if (ghCreds.length === 0) {
        setIsCreatingNewCred(true);
        setActiveStep(1);
      } else {
        setIsCreatingNewCred(false);
        setActiveStep(2);
        if (activeTeamId && targetCredId) {
          fetchReposForCredential(activeTeamId, targetCredId);
        }
      }
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Failed to load integrations');
    } finally {
      setLoading(false);
    }
  }, [activeProjectId, activeTeamId, connectForm, fetchReposForCredential]);

  useEffect(() => {
    if (open && activeProjectId) {
      fetchData();
    }
  }, [open, activeProjectId, fetchData]);

  const handleCredentialChange = (newCredId: string) => {
    connectForm.setFieldValue('credential_id', newCredId);
    if (activeTeamId && newCredId) {
      fetchReposForCredential(activeTeamId, newCredId);
    }
  };

  const handleSelectRepository = (item: GitHubRepoDTO) => {
    connectForm.setFieldValue('repo', item.full_name);
    connectForm.setFieldValue('base_branch', item.default_branch || 'main');
  };

  const filteredRepositories = useMemo(() => {
    const q = repoFilterQuery.trim().toLowerCase();
    if (!q) return gitHubRepos;
    return gitHubRepos.filter(
      (r) =>
        r.name.toLowerCase().includes(q) ||
        r.full_name.toLowerCase().includes(q) ||
        (r.description && r.description.toLowerCase().includes(q))
    );
  }, [gitHubRepos, repoFilterQuery]);

  const handleCopyWebhook = () => {
    navigator.clipboard.writeText(webhookUrl);
    setCopiedWebhook(true);
    setTimeout(() => setCopiedWebhook(false), 2000);
  };

  const handleCopySecret = (secretVal: string) => {
    if (!secretVal) return;
    navigator.clipboard.writeText(secretVal);
    setCopiedSecret(true);
    setTimeout(() => setCopiedSecret(false), 2000);
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
        className="sm:max-w-5xl lg:max-w-6xl max-h-[92vh] overflow-y-auto p-6 w-full"
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
              onClick={() => {
                setShowAddForm(!showAddForm);
                if (!showAddForm && credentials.length === 0) {
                  setIsCreatingNewCred(true);
                  setActiveStep(1);
                }
              }}
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

        {/* Guided Connect Form */}
        {showAddForm && (
          <div
            className="p-4 rounded-lg border border-primary/30 bg-muted/20 space-y-4 animate-in fade-in-50 duration-150"
            data-testid="connect-repo-form"
          >
            {/* Stepper Header */}
            <div className="flex items-center justify-between pb-3 border-b border-border/60">
              <div className="flex items-center gap-2">
                <SlidersHorizontal className="size-4 text-primary" />
                <h4 className="text-xs font-semibold text-foreground">
                  Guided GitHub Connection
                </h4>
              </div>

              {/* Step indicator pills */}
              <div className="flex items-center gap-2 text-[11px]">
                <button
                  type="button"
                  onClick={() => setActiveStep(1)}
                  className={`flex items-center gap-1.5 px-2 py-0.5 rounded-full font-medium transition-colors ${
                    activeStep === 1
                      ? 'bg-primary text-primary-foreground'
                      : credentials.length > 0
                      ? 'bg-muted text-muted-foreground hover:text-foreground'
                      : 'text-muted-foreground'
                  }`}
                  data-testid="step-1-tab"
                >
                  {credentials.length > 0 ? (
                    <CheckCircle2 className="size-3 text-emerald-400" />
                  ) : (
                    <span>1</span>
                  )}
                  <span>PAT Auth</span>
                </button>
                <ChevronRight className="size-3 text-muted-foreground/60" />
                <button
                  type="button"
                  disabled={credentials.length === 0}
                  onClick={() => setActiveStep(2)}
                  className={`flex items-center gap-1 px-2 py-0.5 rounded-full font-medium transition-colors ${
                    activeStep === 2
                      ? 'bg-primary text-primary-foreground'
                      : 'text-muted-foreground hover:text-foreground disabled:opacity-40 disabled:pointer-events-none'
                  }`}
                  data-testid="step-2-tab"
                >
                  <span>2</span>
                  <span>Repository Picker</span>
                </button>
              </div>
            </div>

            {/* STEP 1: GitHub PAT Setup & Verification */}
            {activeStep === 1 && (
              <div className="space-y-3" data-testid="step-1-pat-section">
                <div className="flex items-center justify-between">
                  <div>
                    <h5 className="text-xs font-semibold text-foreground flex items-center gap-1.5">
                      <KeyRound className="size-3.5 text-primary" />
                      Step 1: GitHub Personal Access Token (PAT)
                    </h5>
                    <p className="text-[11px] text-muted-foreground mt-0.5">
                      A PAT enables reading repositories and creating automated PRs.
                    </p>
                  </div>
                  {credentials.length > 0 && !isCreatingNewCred && (
                    <Button
                      type="button"
                      variant="outline"
                      size="xs"
                      onClick={() => setIsCreatingNewCred(true)}
                      className="h-6 text-[11px]"
                    >
                      + Add Another Token
                    </Button>
                  )}
                </div>

                {credentials.length > 0 && !isCreatingNewCred ? (
                  <div className="p-3 rounded-lg border border-border bg-card space-y-3">
                    <div className="flex items-center justify-between">
                      <span className="text-xs font-medium text-foreground">Select Active PAT</span>
                      <Badge variant="outline" className="text-[10px] text-emerald-600 dark:text-emerald-400">
                        Configured
                      </Badge>
                    </div>

                    <connectForm.Field
                      name="credential_id"
                      children={(field) => (
                        <select
                          className="w-full h-8 text-xs font-mono rounded-md border border-input bg-background px-2.5 outline-none focus:border-ring focus:ring-1 focus:ring-ring"
                          value={field.state.value}
                          onChange={(e) => {
                            field.handleChange(e.target.value);
                            handleCredentialChange(e.target.value);
                          }}
                          data-testid="credential-select"
                        >
                          {credentials.map((c) => (
                            <option key={c.id} value={c.id}>
                              {c.label} ({c.id.slice(0, 8)})
                            </option>
                          ))}
                        </select>
                      )}
                    />

                    <div className="flex justify-end pt-1">
                      <Button
                        type="button"
                        size="xs"
                        onClick={() => {
                          setActiveStep(2);
                          const credId = connectForm.getFieldValue('credential_id');
                          if (activeTeamId && credId) {
                            fetchReposForCredential(activeTeamId, credId);
                          }
                        }}
                        className="h-7 text-xs gap-1"
                        data-testid="continue-to-step-2-btn"
                      >
                        <span>Continue to Repository Picker</span>
                        <ChevronRight className="size-3" />
                      </Button>
                    </div>
                  </div>
                ) : (
                  /* New PAT Form using TanStack Form */
                  <form
                    onSubmit={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      patForm.handleSubmit();
                    }}
                    className="p-3 rounded-lg border border-border bg-card space-y-3"
                    data-testid="new-pat-form"
                  >
                    <div className="p-2.5 rounded bg-muted/40 border border-border/60 text-[11px] text-muted-foreground flex items-start gap-2">
                      <Info className="size-3.5 text-primary shrink-0 mt-0.5" />
                      <div>
                        Requires <code className="text-foreground font-mono font-medium">repo</code> and{' '}
                        <code className="text-foreground font-mono font-medium">workflow</code> scopes.
                      </div>
                    </div>

                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5">
                      <patForm.Field
                        name="label"
                        children={(field) => {
                          const errorMsg = field.state.meta.errors[0]
                            ? (typeof field.state.meta.errors[0] === 'string'
                                ? field.state.meta.errors[0]
                                : (field.state.meta.errors[0] as { message?: string })?.message)
                            : undefined;
                          return (
                            <div className="space-y-1">
                              <label htmlFor="new-cred-label" className="text-[11px] font-medium text-foreground">
                                Token Label *
                              </label>
                              <Input
                                id="new-cred-label"
                                placeholder="e.g. Org GitHub Bot or Work PAT"
                                value={field.state.value}
                                onBlur={field.handleBlur}
                                onChange={(e) => field.handleChange(e.target.value)}
                                className="text-xs h-8"
                                data-testid="new-cred-label"
                              />
                              {errorMsg && (
                                <p className="text-[10px] text-destructive font-medium">{errorMsg}</p>
                              )}
                            </div>
                          );
                        }}
                      />

                      <patForm.Field
                        name="secret"
                        children={(field) => {
                          const errorMsg = field.state.meta.errors[0]
                            ? (typeof field.state.meta.errors[0] === 'string'
                                ? field.state.meta.errors[0]
                                : (field.state.meta.errors[0] as { message?: string })?.message)
                            : undefined;
                          return (
                            <div className="space-y-1">
                              <label htmlFor="new-cred-secret" className="text-[11px] font-medium text-foreground">
                                Personal Access Token (Secret) *
                              </label>
                              <Input
                                id="new-cred-secret"
                                type="password"
                                placeholder="ghp_xxxxxxxxxxxx"
                                value={field.state.value}
                                onBlur={field.handleBlur}
                                onChange={(e) => field.handleChange(e.target.value)}
                                className="text-xs h-8 font-mono"
                                data-testid="new-cred-secret"
                              />
                              {errorMsg && (
                                <p className="text-[10px] text-destructive font-medium">{errorMsg}</p>
                              )}
                            </div>
                          );
                        }}
                      />
                    </div>

                    <div className="flex justify-end gap-2 pt-1 border-t border-border/40">
                      {credentials.length > 0 && (
                        <Button
                          type="button"
                          variant="ghost"
                          size="xs"
                          onClick={() => setIsCreatingNewCred(false)}
                          className="h-7 text-xs"
                        >
                          Cancel
                        </Button>
                      )}
                      <patForm.Subscribe
                        selector={(state) => [state.isSubmitting]}
                        children={([isSubmitting]) => (
                          <Button
                            type="submit"
                            size="xs"
                            disabled={isSubmitting}
                            className="h-7 text-xs font-medium"
                            data-testid="save-cred-btn"
                          >
                            {isSubmitting ? 'Verifying & Saving...' : 'Save Token & Continue'}
                          </Button>
                        )}
                      />
                    </div>
                  </form>
                )}
              </div>
            )}

            {/* STEP 2: Interactive Repository Picker & Integration Configuration */}
            {activeStep === 2 && (
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  connectForm.handleSubmit();
                }}
                className="space-y-4"
                data-testid="step-2-repo-section"
              >
                <div>
                  <div className="flex items-center justify-between">
                    <h5 className="text-xs font-semibold text-foreground flex items-center gap-1.5">
                      <GitBranch className="size-3.5 text-primary" />
                      Step 2: Select Repository &amp; Configuration
                    </h5>
                    <button
                      type="button"
                      onClick={() => setManualRepoMode(!manualRepoMode)}
                      className="text-[11px] text-primary hover:underline font-medium"
                      data-testid="toggle-manual-repo"
                    >
                      {manualRepoMode ? 'Choose from list' : 'Or enter repository manually'}
                    </button>
                  </div>
                  <p className="text-[11px] text-muted-foreground mt-0.5">
                    Select a repository accessible with your token, or manually specify owner/repo.
                  </p>
                </div>

                {/* Repository Selection Section */}
                <connectForm.Field
                  name="repo"
                  children={(repoField) => {
                    const repoError = repoField.state.meta.errors[0]
                      ? (typeof repoField.state.meta.errors[0] === 'string'
                          ? repoField.state.meta.errors[0]
                          : (repoField.state.meta.errors[0] as { message?: string })?.message)
                      : undefined;

                    return (
                      <div className="space-y-2">
                        {/* Always keep repo input accessible for manual mode or direct entry */}
                        {manualRepoMode ? (
                          <div className="space-y-1">
                            <label htmlFor="repo-input" className="text-xs font-medium text-foreground">
                              Repository (owner/repo) *
                            </label>
                            <Input
                              id="repo-input"
                              placeholder="e.g. facebook/react or octocat/hello-world"
                              value={repoField.state.value}
                              onBlur={repoField.handleBlur}
                              onChange={(e) => repoField.handleChange(e.target.value)}
                              className="font-mono text-xs h-8"
                              data-testid="repo-input"
                            />
                          </div>
                        ) : (
                          <div className="space-y-2">
                            <div className="relative">
                              <Search className="size-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground" />
                              <Input
                                placeholder="Filter repositories..."
                                value={repoFilterQuery}
                                onChange={(e) => setRepoFilterQuery(e.target.value)}
                                className="pl-8 text-xs h-8"
                                data-testid="repo-search-input"
                              />
                            </div>

                            {loadingRepos ? (
                              <div className="p-6 rounded-md border border-border bg-card text-center text-xs text-muted-foreground">
                                <RefreshCw className="size-4 animate-spin mx-auto mb-1.5 text-muted-foreground" />
                                Fetching repositories from GitHub...
                              </div>
                            ) : gitHubRepos.length === 0 ? (
                              <div className="p-4 rounded-md border border-dashed border-border bg-card text-center text-xs text-muted-foreground space-y-1">
                                <p>No repositories returned for this token.</p>
                                <button
                                  type="button"
                                  onClick={() => setManualRepoMode(true)}
                                  className="text-[11px] text-primary hover:underline font-medium"
                                >
                                  Enter repository manually
                                </button>
                              </div>
                            ) : (
                              <div
                                className="max-h-48 overflow-y-auto rounded-md border border-border bg-card divide-y divide-border/60"
                                data-testid="repo-picker-list"
                              >
                                {filteredRepositories.length === 0 ? (
                                  <div className="p-3 text-center text-xs text-muted-foreground">
                                    No matching repositories found.
                                  </div>
                                ) : (
                                  filteredRepositories.map((item) => {
                                    const isSelected = repoField.state.value === item.full_name;
                                    return (
                                      <button
                                        key={item.id}
                                        type="button"
                                        onClick={() => handleSelectRepository(item)}
                                        className={`w-full text-left p-2.5 transition-colors flex items-center justify-between gap-3 text-xs ${
                                          isSelected
                                            ? 'bg-primary/10 text-foreground font-medium'
                                            : 'hover:bg-muted/50 text-foreground'
                                        }`}
                                        data-testid={`repo-item-${item.full_name}`}
                                      >
                                        <div className="min-w-0 flex-1">
                                          <div className="flex items-center gap-1.5 font-mono text-xs">
                                            {item.private && (
                                              <Lock className="size-3 text-muted-foreground shrink-0" />
                                            )}
                                            <span className="truncate">{item.full_name}</span>
                                            <Badge
                                              variant="secondary"
                                              className="text-[10px] px-1 py-0 h-4 font-mono font-normal ml-1"
                                            >
                                              {item.default_branch}
                                            </Badge>
                                          </div>
                                          {item.description && (
                                            <p className="text-[11px] text-muted-foreground truncate mt-0.5">
                                              {item.description}
                                            </p>
                                          )}
                                        </div>
                                        {isSelected && (
                                          <Check className="size-4 text-primary shrink-0" />
                                        )}
                                      </button>
                                    );
                                  })
                                )}
                              </div>
                            )}

                            {/* Direct manual input option also available / visible */}
                            <div className="pt-1">
                              <label htmlFor="repo-input" className="text-[11px] font-medium text-foreground block mb-1">
                                Repository Name *
                              </label>
                              <Input
                                id="repo-input"
                                placeholder="owner/repo (or select from list above)"
                                value={repoField.state.value}
                                onBlur={repoField.handleBlur}
                                onChange={(e) => repoField.handleChange(e.target.value)}
                                className="font-mono text-xs h-8"
                                data-testid="repo-input"
                              />
                            </div>
                          </div>
                        )}

                        {repoError && (
                          <p className="text-[11px] text-destructive font-medium">{repoError}</p>
                        )}
                      </div>
                    );
                  }}
                />

                {/* Webhook Setup Snippet */}
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

                  <connectForm.Field
                    name="webhook_secret"
                    children={(field) => (
                      <div className="space-y-1.5 pt-1">
                        <div className="flex items-center justify-between">
                          <label htmlFor="webhook-secret-input" className="text-[11px] font-medium text-foreground">
                            Webhook Secret (Optional)
                          </label>
                          {field.state.value && (
                            <Button
                              type="button"
                              variant="ghost"
                              size="xs"
                              onClick={() => handleCopySecret(field.state.value || '')}
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
                          value={field.state.value || ''}
                          onBlur={field.handleBlur}
                          onChange={(e) => field.handleChange(e.target.value)}
                          className="font-mono text-[11px] h-7"
                          data-testid="webhook-secret-input"
                        />
                      </div>
                    )}
                  />
                </div>

                {/* Settings */}
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 pt-1">
                  <connectForm.Field
                    name="base_branch"
                    children={(field) => (
                      <div className="space-y-1">
                        <label htmlFor="base-branch-input" className="text-xs font-medium text-foreground">
                          Base Branch
                        </label>
                        <Input
                          id="base-branch-input"
                          placeholder="main"
                          value={field.state.value}
                          onBlur={field.handleBlur}
                          onChange={(e) => field.handleChange(e.target.value)}
                          className="text-xs h-8 font-mono"
                          data-testid="base-branch-input"
                        />
                      </div>
                    )}
                  />

                  <connectForm.Field
                    name="filter_label"
                    children={(field) => (
                      <div className="space-y-1">
                        <label htmlFor="filter-label-input" className="text-xs font-medium text-foreground">
                          Filter Label (Optional)
                        </label>
                        <Input
                          id="filter-label-input"
                          placeholder="e.g. agent or auto-task"
                          value={field.state.value || ''}
                          onBlur={field.handleBlur}
                          onChange={(e) => field.handleChange(e.target.value)}
                          className="text-xs h-8 font-mono"
                          data-testid="filter-label-input"
                        />
                      </div>
                    )}
                  />
                </div>

                {/* Auto PR Checkbox */}
                <connectForm.Field
                  name="auto_pr"
                  children={(field) => (
                    <div className="flex items-center gap-2 pt-1">
                      <input
                        type="checkbox"
                        id="auto-pr-checkbox"
                        checked={field.state.value}
                        onChange={(e) => field.handleChange(e.target.checked)}
                        className="size-4 rounded border-input text-primary focus:ring-ring"
                        data-testid="auto-pr-checkbox"
                      />
                      <label htmlFor="auto-pr-checkbox" className="text-xs text-foreground cursor-pointer">
                        Auto-create Pull Request on completion
                      </label>
                    </div>
                  )}
                />

                {/* Actions */}
                <div className="flex items-center justify-between pt-2 border-t border-border/40">
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => setActiveStep(1)}
                    className="text-xs"
                  >
                    Back to Token
                  </Button>
                  <div className="flex items-center gap-2">
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => setShowAddForm(false)}
                      className="text-xs"
                    >
                      Cancel
                    </Button>
                    <connectForm.Subscribe
                      selector={(state) => [state.isSubmitting]}
                      children={([isSubmitting]) => (
                        <Button
                          type="submit"
                          size="sm"
                          disabled={isSubmitting}
                          className="text-xs font-medium"
                          data-testid="connect-submit-btn"
                        >
                          {isSubmitting ? 'Connecting...' : 'Connect Repository'}
                        </Button>
                      )}
                    />
                  </div>
                </div>
              </form>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
};