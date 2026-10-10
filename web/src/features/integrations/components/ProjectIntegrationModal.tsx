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
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs';
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
  AlertTriangle,
  RefreshCw,
  Lock,
  Search,
  CheckCircle2,
  Eye,
  EyeOff,
  ShieldCheck,
  ExternalLink as LinkIcon,
} from 'lucide-react';

export interface ProjectIntegrationModalProps {
  trigger?: React.ReactNode;
}

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

  // Tab State
  const [activeTab, setActiveTab] = useState<'repos' | 'pat'>('repos');

  // Core Data
  const [integrations, setIntegrations] = useState<IntegrationDTO[]>([]);
  const [credentials, setCredentials] = useState<CredentialDTO[]>([]);
  const [loading, setLoading] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionSuccess, setActionSuccess] = useState<string | null>(null);

  // Token Validation State
  const [tokenStatus, setTokenStatus] = useState<'valid' | 'invalid' | 'unknown' | 'testing'>('unknown');
  const [testingCredId, setTestingCredId] = useState<string | null>(null);

  // Token Form State (Add / Update)
  const [showTokenForm, setShowTokenForm] = useState(false);
  const [editingCred, setEditingCred] = useState<CredentialDTO | null>(null);
  const [tokenLabel, setTokenLabel] = useState('');
  const [tokenSecret, setTokenSecret] = useState('');
  const [showSecret, setShowSecret] = useState(false);
  const [savingToken, setSavingToken] = useState(false);
  const [tokenFormError, setTokenFormError] = useState<string | null>(null);

  // Repositories View State
  const [showAddRepoForm, setShowAddRepoForm] = useState(false);
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

  // Connect Repo Form
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
      setActionSuccess(null);
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
        setShowAddRepoForm(false);
        setActionSuccess(`Connected ${created.external_id} successfully!`);
      } catch (err) {
        setActionError(err instanceof Error ? err.message : 'Failed to connect repository');
      }
    },
  });

  const fetchReposForCredential = useCallback(async (teamId: string, credId: string) => {
    if (!teamId || !credId) return;
    setLoadingRepos(true);
    try {
      const repos = await credentialApi.listGitHubRepositories(teamId, credId);
      setGitHubRepos(repos);
      setTokenStatus('valid');
      return repos;
    } catch (err) {
      setTokenStatus('invalid');
      const msg = err instanceof Error ? err.message : 'Failed to list GitHub repositories';
      setActionError(msg);
      throw err;
    } finally {
      setLoadingRepos(false);
    }
  }, []);

  const fetchData = useCallback(async () => {
    if (!activeProjectId) return;
    setLoading(true);
    setActionError(null);
    setActionSuccess(null);
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
        setTokenStatus('unknown');
        setShowTokenForm(true);
        if (integs.length === 0) {
          setActiveTab('pat');
        }
      } else if (activeTeamId && targetCredId) {
        fetchReposForCredential(activeTeamId, targetCredId).catch(() => {
          // fetchReposForCredential already updates tokenStatus to 'invalid' & setActionError
        });
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

  const handleTestToken = async (credId: string) => {
    if (!activeTeamId) return;
    setTestingCredId(credId);
    setActionError(null);
    setActionSuccess(null);
    try {
      const repos = await credentialApi.listGitHubRepositories(activeTeamId, credId);
      setGitHubRepos(repos);
      setTokenStatus('valid');
      setActionSuccess(`✓ Token verified successfully! Found ${repos.length} accessible repositories.`);
    } catch (err) {
      setTokenStatus('invalid');
      setActionError(err instanceof Error ? err.message : 'Token verification failed');
    } finally {
      setTestingCredId(null);
    }
  };

  const handleOpenEditToken = (cred: CredentialDTO) => {
    setEditingCred(cred);
    setTokenLabel(cred.label);
    setTokenSecret('');
    setShowTokenForm(true);
    setTokenFormError(null);
    setActionError(null);
    setActiveTab('pat');
  };

  const handleOpenNewToken = () => {
    setEditingCred(null);
    setTokenLabel(credentials.length === 0 ? 'GitHub Personal Access Token' : `PAT #${credentials.length + 1}`);
    setTokenSecret('');
    setShowTokenForm(true);
    setTokenFormError(null);
    setActionError(null);
    setActiveTab('pat');
  };

  const handleSaveToken = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!activeTeamId) {
      setTokenFormError('Active team required');
      return;
    }
    const trimmedLabel = tokenLabel.trim();
    const trimmedSecret = tokenSecret.trim();

    if (!trimmedLabel) {
      setTokenFormError('Token label is required');
      return;
    }

    if (!editingCred && !trimmedSecret) {
      setTokenFormError('Personal Access Token secret is required');
      return;
    }

    setSavingToken(true);
    setTokenFormError(null);
    setActionError(null);

    try {
      let savedCred: CredentialDTO;
      if (editingCred) {
        // Updating existing credential
        savedCred = await credentialApi.updateCredential(activeTeamId, editingCred.id, {
          label: trimmedLabel,
          secret: trimmedSecret || undefined,
        });
        setCredentials((prev) => prev.map((c) => (c.id === savedCred.id ? savedCred : c)));
      } else {
        // Creating new credential
        savedCred = await credentialApi.createCredential(activeTeamId, {
          provider: 'github',
          label: trimmedLabel,
          secret: trimmedSecret,
        });
        setCredentials((prev) => [...prev, savedCred]);
      }

      connectForm.setFieldValue('credential_id', savedCred.id);
      setShowTokenForm(false);
      setEditingCred(null);
      setTokenSecret('');

      // Test token immediately
      try {
        const repos = await credentialApi.listGitHubRepositories(activeTeamId, savedCred.id);
        setGitHubRepos(repos);
        setTokenStatus('valid');
        setActionSuccess(`✓ Token saved and verified! Found ${repos.length} accessible repositories.`);
      } catch (testErr) {
        setTokenStatus('invalid');
        setActionError(testErr instanceof Error ? testErr.message : 'Token saved, but verification failed');
      }
    } catch (err) {
      setTokenFormError(err instanceof Error ? err.message : 'Failed to save credential');
    } finally {
      setSavingToken(false);
    }
  };

  const handleDeleteToken = async (credId: string) => {
    if (!activeTeamId) return;
    if (!confirm('Are you sure you want to delete this GitHub token? Associated integrations may stop functioning.')) {
      return;
    }
    setActionError(null);
    try {
      await credentialApi.deleteCredential(activeTeamId, credId);
      const remaining = credentials.filter((c) => c.id !== credId);
      setCredentials(remaining);
      if (remaining.length > 0) {
        connectForm.setFieldValue('credential_id', remaining[0].id);
      } else {
        connectForm.setFieldValue('credential_id', '');
        setTokenStatus('unknown');
        setShowTokenForm(true);
      }
      setActionSuccess('Token deleted successfully');
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Failed to delete token');
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
      setActionSuccess('Repository integration removed');
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Failed to delete integration');
    }
  };

  const isTokenExpired =
    tokenStatus === 'invalid' ||
    (actionError && actionError.toLowerCase().includes('token'));

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
        className="sm:max-w-4xl lg:max-w-5xl max-h-[92vh] overflow-y-auto p-6 w-full"
        data-testid="project-integration-modal"
      >
        <DialogHeader className="pb-3 border-b border-border">
          <div className="flex items-center justify-between gap-3 pr-6">
            <div className="flex items-center gap-2.5">
              <div className="size-8 rounded-lg bg-primary/10 text-primary border border-primary/20 flex items-center justify-center">
                <GitBranch className="size-4" />
              </div>
              <div>
                <DialogTitle className="text-base font-semibold leading-none">
                  Project Integrations
                </DialogTitle>
                <DialogDescription className="text-xs text-muted-foreground mt-1">
                  Connect GitHub repositories and configure your Personal Access Token (PAT).
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

        {/* Actionable Token Error Banner */}
        {isTokenExpired && (
          <div
            className="flex items-start gap-3 p-3.5 rounded-lg border border-destructive/30 bg-destructive/10 text-destructive animate-in fade-in-50"
            data-testid="token-error-banner"
          >
            <AlertTriangle className="size-5 shrink-0 mt-0.5 text-destructive" />
            <div className="flex-1 space-y-1">
              <div className="font-semibold text-xs text-destructive">
                Invalid or Expired GitHub Token
              </div>
              <p className="text-xs text-destructive/90 leading-relaxed">
                Your Personal Access Token cannot authenticate with GitHub. Autonomous task execution,
                repository sync, and automated PRs are currently paused until a valid token is provided.
              </p>
              <div className="flex items-center gap-2 pt-1">
                <Button
                  size="xs"
                  variant="destructive"
                  className="h-7 text-xs font-medium gap-1"
                  onClick={() => {
                    setActiveTab('pat');
                    if (credentials.length > 0) {
                      handleOpenEditToken(credentials[0]);
                    } else {
                      handleOpenNewToken();
                    }
                  }}
                  data-testid="fix-token-btn"
                >
                  <KeyRound className="size-3" />
                  <span>Update PAT Now</span>
                </Button>
                <a
                  href="https://github.com/settings/tokens/new?scopes=repo,workflow&description=AgentTaskManager"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-xs text-destructive hover:underline font-medium inline-flex items-center gap-1 ml-2"
                >
                  <span>Generate new token on GitHub</span>
                  <LinkIcon className="size-3" />
                </a>
              </div>
            </div>
          </div>
        )}

        {/* Generic Action Error (if not token error) */}
        {actionError && !isTokenExpired && (
          <div className="flex items-start gap-2 p-3 text-xs bg-destructive/10 border border-destructive/20 rounded-md text-destructive">
            <AlertCircle className="size-4 shrink-0 mt-0.5" />
            <div className="flex-1 font-medium">{actionError}</div>
          </div>
        )}

        {/* Success Banner */}
        {actionSuccess && (
          <div className="flex items-start gap-2 p-3 text-xs bg-emerald-500/10 border border-emerald-500/20 rounded-md text-emerald-600 dark:text-emerald-400">
            <CheckCircle2 className="size-4 shrink-0 mt-0.5" />
            <div className="flex-1 font-medium">{actionSuccess}</div>
          </div>
        )}

        {/* Tab Navigation */}
        <Tabs
          value={activeTab}
          onValueChange={(val) => setActiveTab(val as 'repos' | 'pat')}
          className="w-full space-y-4 pt-1"
        >
          <TabsList className="grid grid-cols-2 w-full max-w-md h-9">
            <TabsTrigger
              value="repos"
              className="text-xs flex items-center justify-center gap-2"
              data-testid="tab-repositories"
            >
              <GitBranch className="size-3.5" />
              <span>Repositories</span>
              <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4 font-mono font-medium">
                {integrations.length}
              </Badge>
            </TabsTrigger>

            <TabsTrigger
              value="pat"
              className="text-xs flex items-center justify-center gap-2"
              data-testid="tab-pat"
            >
              <KeyRound className="size-3.5" />
              <span>GitHub Token (PAT)</span>
              {isTokenExpired ? (
                <Badge variant="destructive" className="text-[9px] px-1 py-0 h-3.5 font-medium animate-pulse">
                  Expired
                </Badge>
              ) : credentials.length > 0 ? (
                <Badge variant="outline" className="text-[9px] px-1 py-0 h-3.5 font-medium text-emerald-600 dark:text-emerald-400">
                  Active
                </Badge>
              ) : (
                <Badge variant="secondary" className="text-[9px] px-1 py-0 h-3.5 font-medium">
                  Setup Needed
                </Badge>
              )}
            </TabsTrigger>
          </TabsList>

          {/* ============================================================ */}
          {/* TAB 1: REPOSITORIES VIEW */}
          {/* ============================================================ */}
          <TabsContent value="repos" className="space-y-4 mt-2">
            <div className="flex items-center justify-between">
              <div>
                <h4 className="text-xs font-semibold text-foreground uppercase tracking-wider flex items-center gap-1.5">
                  <GitBranch className="size-3.5 text-primary" /> Connected Repositories
                </h4>
                <p className="text-[11px] text-muted-foreground mt-0.5">
                  Repositories linked to this project for code editing, commits, and PR automation.
                </p>
              </div>

              <Button
                size="xs"
                variant={showAddRepoForm ? 'secondary' : 'default'}
                onClick={() => {
                  setShowAddRepoForm(!showAddRepoForm);
                  if (!showAddRepoForm && credentials.length === 0) {
                    setActiveTab('pat');
                  }
                }}
                className="h-7 text-xs gap-1"
                data-testid="toggle-connect-repo-btn"
              >
                <Plus className="size-3" />
                {showAddRepoForm ? 'Close Form' : 'Connect GitHub Repository'}
              </Button>
            </div>

            {/* Repositories List */}
            {loading ? (
              <div className="p-8 text-center text-xs text-muted-foreground">
                <RefreshCw className="size-4 animate-spin mx-auto mb-2 text-muted-foreground" />
                Loading integrations...
              </div>
            ) : integrations.length === 0 ? (
              <div className="p-8 rounded-lg border border-dashed border-border bg-muted/20 text-center text-xs text-muted-foreground space-y-2">
                <GitBranch className="size-7 mx-auto text-muted-foreground/60" />
                <p className="font-medium text-foreground">No GitHub repositories connected yet</p>
                <p className="max-w-md mx-auto">
                  Click "Connect GitHub Repository" to link a repo. Make sure your GitHub Personal Access Token is configured first.
                </p>
                {credentials.length === 0 && (
                  <Button
                    size="xs"
                    variant="outline"
                    onClick={() => setActiveTab('pat')}
                    className="mt-2 text-xs gap-1.5"
                  >
                    <KeyRound className="size-3 text-primary" />
                    Configure GitHub Token (PAT)
                  </Button>
                )}
              </div>
            ) : (
              <div className="space-y-2">
                {integrations.map((item) => (
                  <div
                    key={item.id}
                    className="flex items-center justify-between gap-3 p-3 rounded-lg border border-border bg-card shadow-xs hover:border-border/80 transition-colors"
                    data-testid={`integration-row-${item.id}`}
                  >
                    <div className="flex items-center gap-3 min-w-0">
                      <div className="size-8 rounded-md bg-muted/60 flex items-center justify-center shrink-0">
                        <GitBranch className="size-4 text-primary" />
                      </div>
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="font-semibold text-xs text-foreground font-mono truncate">
                            {item.external_id}
                          </span>
                          {item.enabled ? (
                            <Badge className="bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border border-emerald-500/30 text-[10px] font-mono py-0 h-4">
                              Active
                            </Badge>
                          ) : (
                            <Badge variant="outline" className="text-[10px] font-mono py-0 h-4">
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

            {/* Connect Repository Form Drawer */}
            {showAddRepoForm && (
              <div
                className="p-4 rounded-lg border border-primary/30 bg-muted/15 space-y-4 animate-in fade-in-50 duration-150"
                data-testid="connect-repo-form"
              >
                <div className="flex items-center justify-between pb-3 border-b border-border/60">
                  <div className="flex items-center gap-2">
                    <GitBranch className="size-4 text-primary" />
                    <h5 className="text-xs font-semibold text-foreground">
                      Connect a GitHub Repository
                    </h5>
                  </div>
                  <button
                    type="button"
                    onClick={() => setManualRepoMode(!manualRepoMode)}
                    className="text-[11px] text-primary hover:underline font-medium"
                    data-testid="toggle-manual-repo"
                  >
                    {manualRepoMode ? 'Browse accessible repos' : 'Enter owner/repo manually'}
                  </button>
                </div>

                {/* Token Notice */}
                {credentials.length > 0 ? (
                  <div className="flex items-center justify-between px-3 py-2 rounded-md bg-muted/40 border border-border/60 text-xs">
                    <div className="flex items-center gap-2 text-muted-foreground">
                      <KeyRound className="size-3.5 text-primary shrink-0" />
                      <span>
                        Using PAT:{' '}
                        <b className="text-foreground font-mono">
                          {credentials.find((c) => c.id === connectForm.getFieldValue('credential_id'))?.label ||
                            credentials[0].label}
                        </b>
                      </span>
                    </div>
                    <button
                      type="button"
                      onClick={() => setActiveTab('pat')}
                      className="text-[11px] text-primary hover:underline font-medium"
                    >
                      Change or update token
                    </button>
                  </div>
                ) : (
                  <div className="flex items-center justify-between p-3 rounded-md bg-amber-500/10 border border-amber-500/20 text-xs text-amber-700 dark:text-amber-400">
                    <div className="flex items-center gap-2">
                      <AlertTriangle className="size-4 shrink-0" />
                      <span>No GitHub Personal Access Token configured.</span>
                    </div>
                    <Button
                      size="xs"
                      variant="outline"
                      onClick={() => setActiveTab('pat')}
                      className="h-6 text-[11px]"
                    >
                      Configure Token Now
                    </Button>
                  </div>
                )}

                <form
                  onSubmit={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    connectForm.handleSubmit();
                  }}
                  className="space-y-4"
                >
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
                                  <p>No repositories returned from GitHub.</p>
                                  <button
                                    type="button"
                                    onClick={() => setManualRepoMode(true)}
                                    className="text-[11px] text-primary hover:underline font-medium"
                                  >
                                    Enter repository name manually
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

                              {/* Manual input fallback always accessible */}
                              <div className="pt-1">
                                <label htmlFor="repo-input" className="text-[11px] font-medium text-foreground block mb-1">
                                  Selected Repository *
                                </label>
                                <Input
                                  id="repo-input"
                                  placeholder="owner/repo (or click repository in list above)"
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

                  {/* Webhook Configuration Snippet */}
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
                      In your GitHub Repo &gt; <b>Settings &gt; Webhooks &gt; Add webhook</b>, set Payload URL to the URL below, content type to <code className="font-mono text-foreground">application/json</code>, and trigger on <code className="font-mono text-foreground">Issues</code>.
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

                  {/* Branch and Auto-PR Settings */}
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
                          Auto-create Pull Request upon task completion
                        </label>
                      </div>
                    )}
                  />

                  {/* Form Footer */}
                  <div className="flex items-center justify-end gap-2 pt-2 border-t border-border/40">
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => setShowAddRepoForm(false)}
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
                </form>
              </div>
            )}
          </TabsContent>

          {/* ============================================================ */}
          {/* TAB 2: GITHUB AUTHENTICATION & PAT MANAGEMENT */}
          {/* ============================================================ */}
          <TabsContent value="pat" className="space-y-4 mt-2">
            <div className="flex items-center justify-between">
              <div>
                <h4 className="text-xs font-semibold text-foreground uppercase tracking-wider flex items-center gap-1.5">
                  <KeyRound className="size-3.5 text-primary" /> GitHub Personal Access Tokens (PAT)
                </h4>
                <p className="text-[11px] text-muted-foreground mt-0.5">
                  Secure credentials used to clone code into isolated sandbox containers, commit changes, and open PRs.
                </p>
              </div>

              {!showTokenForm && (
                <Button
                  size="xs"
                  variant="default"
                  onClick={handleOpenNewToken}
                  className="h-7 text-xs gap-1"
                  data-testid="add-token-btn"
                >
                  <Plus className="size-3" />
                  <span>Add Another Token</span>
                </Button>
              )}
            </div>

            {/* List Existing Tokens */}
            {credentials.length > 0 && !showTokenForm && (
              <div className="space-y-3">
                {credentials.map((c) => {
                  const isCurrentTarget = connectForm.getFieldValue('credential_id') === c.id;
                  const isTesting = testingCredId === c.id;

                  return (
                    <div
                      key={c.id}
                      className="p-3.5 rounded-lg border border-border bg-card shadow-xs space-y-3"
                      data-testid={`token-card-${c.id}`}
                    >
                      <div className="flex items-start justify-between gap-3">
                        <div className="flex items-start gap-2.5 min-w-0">
                          <div className="size-8 rounded-md bg-primary/10 text-primary border border-primary/20 flex items-center justify-center shrink-0 mt-0.5">
                            <KeyRound className="size-4" />
                          </div>
                          <div className="min-w-0 space-y-1">
                            <div className="flex items-center gap-2">
                              <span className="font-semibold text-xs text-foreground truncate">
                                {c.label}
                              </span>
                              {isCurrentTarget && (
                                <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4 font-mono">
                                  Default
                                </Badge>
                              )}
                              {tokenStatus === 'valid' && (
                                <Badge className="bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border border-emerald-500/30 text-[10px] font-mono py-0 h-4 flex items-center gap-1">
                                  <Check className="size-2.5" />
                                  <span>Verified &amp; Active</span>
                                </Badge>
                              )}
                              {tokenStatus === 'invalid' && (
                                <Badge variant="destructive" className="text-[10px] font-mono py-0 h-4 flex items-center gap-1">
                                  <AlertCircle className="size-2.5" />
                                  <span>Invalid / Expired</span>
                                </Badge>
                              )}
                            </div>
                            <div className="flex items-center gap-3 text-[11px] text-muted-foreground font-mono">
                              <span>Token ID: {c.id.slice(0, 8)}...</span>
                              <span>•</span>
                              <span>Secret: ghp_••••••••••••••••</span>
                            </div>
                          </div>
                        </div>

                        {/* Actions */}
                        <div className="flex items-center gap-1.5 shrink-0">
                          <Button
                            type="button"
                            size="xs"
                            variant="outline"
                            onClick={() => handleTestToken(c.id)}
                            disabled={isTesting}
                            className="h-7 text-xs gap-1"
                            data-testid={`test-token-btn-${c.id}`}
                          >
                            {isTesting ? (
                              <RefreshCw className="size-3 animate-spin text-muted-foreground" />
                            ) : (
                              <ShieldCheck className="size-3 text-emerald-500" />
                            )}
                            <span>{isTesting ? 'Testing...' : 'Test Connection'}</span>
                          </Button>

                          <Button
                            type="button"
                            size="xs"
                            variant="secondary"
                            onClick={() => handleOpenEditToken(c)}
                            className="h-7 text-xs gap-1"
                            data-testid={`edit-token-btn-${c.id}`}
                          >
                            <KeyRound className="size-3" />
                            <span>Update / Replace</span>
                          </Button>

                          <Button
                            type="button"
                            size="icon-xs"
                            variant="ghost"
                            onClick={() => handleDeleteToken(c.id)}
                            className="text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                            title="Delete Token"
                            data-testid={`delete-token-btn-${c.id}`}
                          >
                            <Trash2 className="size-3.5" />
                          </Button>
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}

            {/* Token Input / Edit Form */}
            {showTokenForm && (
              <form
                onSubmit={handleSaveToken}
                className="p-4 rounded-lg border border-primary/30 bg-muted/20 space-y-4 animate-in fade-in-50"
                data-testid="token-edit-form"
              >
                <div className="flex items-center justify-between pb-2 border-b border-border/60">
                  <div className="flex items-center gap-2">
                    <KeyRound className="size-4 text-primary" />
                    <h5 className="text-xs font-semibold text-foreground">
                      {editingCred ? `Update Token: ${editingCred.label}` : 'Configure GitHub Personal Access Token'}
                    </h5>
                  </div>
                  {credentials.length > 0 && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="xs"
                      onClick={() => {
                        setShowTokenForm(false);
                        setEditingCred(null);
                      }}
                      className="h-6 text-xs"
                    >
                      Cancel
                    </Button>
                  )}
                </div>

                {/* Helper Card */}
                <div className="p-3 rounded-lg bg-card border border-border text-xs space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="font-semibold text-foreground flex items-center gap-1.5">
                      <ShieldCheck className="size-3.5 text-emerald-500" />
                      Required Token Scopes
                    </span>
                    <a
                      href="https://github.com/settings/tokens/new?scopes=repo,workflow&description=AgentTaskManager"
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-[11px] text-primary hover:underline font-medium inline-flex items-center gap-1"
                    >
                      <span>Generate Token on GitHub (pre-configured)</span>
                      <LinkIcon className="size-3" />
                    </a>
                  </div>
                  <p className="text-[11px] text-muted-foreground leading-relaxed">
                    Make sure to enable <code className="text-foreground font-mono font-semibold">repo</code> (Full control of private repositories) and <code className="text-foreground font-mono font-semibold">workflow</code> (Update GitHub Action workflows). Classic PATs (<code className="font-mono text-muted-foreground">ghp_...</code>) or Fine-Grained PATs (<code className="font-mono text-muted-foreground">github_pat_...</code>) are both supported.
                  </p>
                </div>

                {tokenFormError && (
                  <div className="p-2.5 rounded bg-destructive/10 border border-destructive/20 text-xs text-destructive flex items-center gap-2 font-medium">
                    <AlertCircle className="size-3.5 shrink-0" />
                    <span>{tokenFormError}</span>
                  </div>
                )}

                <div className="space-y-3">
                  <div className="space-y-1">
                    <label htmlFor="token-label-input" className="text-xs font-medium text-foreground">
                      Token Label *
                    </label>
                    <Input
                      id="token-label-input"
                      placeholder="e.g. My Personal GitHub PAT or Bot Account"
                      value={tokenLabel}
                      onChange={(e) => setTokenLabel(e.target.value)}
                      className="text-xs h-8"
                      data-testid="token-label-input"
                    />
                  </div>

                  <div className="space-y-1">
                    <div className="flex items-center justify-between">
                      <label htmlFor="token-secret-input" className="text-xs font-medium text-foreground">
                        Personal Access Token (PAT) *
                      </label>
                      {editingCred && (
                        <span className="text-[10px] text-muted-foreground">
                          Leave empty to keep existing token
                        </span>
                      )}
                    </div>
                    <div className="relative">
                      <Input
                        id="token-secret-input"
                        type={showSecret ? 'text' : 'password'}
                        placeholder="ghp_xxxxxxxxxxxxxxxxxxxx or github_pat_xxxxxxxx"
                        value={tokenSecret}
                        onChange={(e) => setTokenSecret(e.target.value)}
                        className="text-xs h-8 font-mono pr-8 select-all"
                        data-testid="token-secret-input"
                      />
                      <button
                        type="button"
                        onClick={() => setShowSecret(!showSecret)}
                        className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                        title={showSecret ? 'Hide secret' : 'Show secret'}
                      >
                        {showSecret ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
                      </button>
                    </div>
                  </div>
                </div>

                <div className="flex items-center justify-end gap-2 pt-2 border-t border-border/40">
                  {credentials.length > 0 && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        setShowTokenForm(false);
                        setEditingCred(null);
                      }}
                      className="text-xs"
                    >
                      Cancel
                    </Button>
                  )}
                  <Button
                    type="submit"
                    size="sm"
                    disabled={savingToken}
                    className="text-xs font-medium gap-1.5"
                    data-testid="save-token-btn"
                  >
                    {savingToken ? (
                      <>
                        <RefreshCw className="size-3 animate-spin" />
                        <span>Verifying &amp; Saving...</span>
                      </>
                    ) : (
                      <>
                        <ShieldCheck className="size-3.5" />
                        <span>{editingCred ? 'Update & Test Token' : 'Save & Verify Token'}</span>
                      </>
                    )}
                  </Button>
                </div>
              </form>
            )}
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
};
