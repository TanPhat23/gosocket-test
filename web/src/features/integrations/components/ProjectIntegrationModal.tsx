import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { useForm, useStore } from '@tanstack/react-form';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
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
import { cn } from 'cn';
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table';
import { BranchCommandPicker } from './BranchCommandPicker';
import {
  useActiveProjectId,
  useProjectsList,
  useActiveTeamId,
} from '@/features/teams/store/useTeamStore';
import { integrationApi } from '../api/integrationApi';
import { credentialApi } from '@/features/teams/api/credentialApi';
import {
  projectIntegrationsQueryOptions,
  credentialsQueryOptions,
  gitHubReposQueryOptions,
  gitHubBranchesQueryOptions,
} from '../queries';
import type { IntegrationDTO, CreateIntegrationInput, UpdateIntegrationInput } from '../schemas/integrationSchemas';
import type { CredentialDTO, GitHubRepoDTO, CreateCredentialInput } from '@/features/teams/schemas/credentialSchemas';
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
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const activeProjectId = useActiveProjectId();
  const activeTeamId = useActiveTeamId();
  const projects = useProjectsList();

  const currentProject = projects.find((p) => p.id === activeProjectId);

  // Tab State: 'repos' (Connected Repositories), 'connect' (+ Connect Repository), 'pat' (GitHub Token)
  const [activeTab, setActiveTab] = useState<'repos' | 'connect' | 'pat'>('repos');

  // Local notification banners
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
  const [tokenFormError, setTokenFormError] = useState<string | null>(null);

  // Connect View State
  const [repoFilterQuery, setRepoFilterQuery] = useState('');
  const [manualRepoMode, setManualRepoMode] = useState(false);
  const [repoScopeFilter, setRepoScopeFilter] = useState<'all' | 'connected' | 'my'>('all');

  // Branch Management State
  const [connectedRepoBranches, setConnectedRepoBranches] = useState<Record<string, string[]>>({});
  const [loadingBranchesForInteg, setLoadingBranchesForInteg] = useState<Record<string, boolean>>({});
  const [updatingBranchIntegId, setUpdatingBranchIntegId] = useState<string | null>(null);

  const [copiedWebhook, setCopiedWebhook] = useState(false);
  const [copiedSecret, setCopiedSecret] = useState(false);

  const webhookUrl =
    typeof window !== 'undefined'
      ? `${window.location.protocol}//${window.location.host}/api/v1/webhooks/github`
      : 'http://localhost:8080/api/v1/webhooks/github';

  // --- QUERIES ---
  const {
    data: rawIntegrations,
    isLoading: loadingIntegrations,
    error: integrationsError,
  } = useQuery({
    ...projectIntegrationsQueryOptions(activeProjectId || ''),
    enabled: Boolean(activeProjectId),
  });

  const integrations = useMemo(() => rawIntegrations || [], [rawIntegrations]);

  const {
    data: rawCredentials,
    isLoading: loadingCredentials,
    error: credentialsError,
  } = useQuery({
    ...credentialsQueryOptions(activeTeamId || ''),
    enabled: Boolean(activeTeamId),
  });

  const gitHubCreds = useMemo(
    () => (rawCredentials || []).filter((c) => c.provider === 'github'),
    [rawCredentials]
  );
  const credentials = gitHubCreds;

  const loading = loadingIntegrations || loadingCredentials;

  // Selected credential for new repo connect form
  const [selectedCredId, setSelectedCredId] = useState<string>('');
  const activeCredId = selectedCredId || (gitHubCreds.length > 0 ? gitHubCreds[0].id : '');

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
      createIntegrationMutation.mutate({
        projectId: activeProjectId,
        data: {
          credential_id: value.credential_id,
          provider: 'github',
          repo: value.repo.trim(),
          base_branch: value.base_branch.trim() || 'main',
          auto_pr: value.auto_pr,
          webhook_secret: value.webhook_secret?.trim() || undefined,
          filter_label: value.filter_label?.trim() || undefined,
        },
      });
    },
  });

  // Reactive subscription to repo field in connectForm
  const formRepoValue = useStore(connectForm.store, (s) => s.values.repo);

  // Query repositories for the selected credential
  const {
    data: queriedRepos,
    isLoading: loadingRepos,
    error: reposQueryError,
  } = useQuery({
    ...gitHubReposQueryOptions(activeTeamId || '', activeCredId),
    enabled: Boolean(activeTeamId && activeCredId),
    retry: false,
  });

  const gitHubRepos = useMemo(() => queriedRepos || [], [queriedRepos]);

  // Handle repository query error or success for tokenStatus
  useEffect(() => {
    if (reposQueryError) {
      setTokenStatus('invalid');
      const msg = reposQueryError instanceof Error ? reposQueryError.message : 'Failed to list GitHub repositories';
      setActionError(msg);
    } else if (queriedRepos) {
      setTokenStatus('valid');
    }
  }, [reposQueryError, queriedRepos]);

  // Sync credential_id in form when credentials load
  useEffect(() => {
    if (credentials.length > 0) {
      const currentVal = connectForm.getFieldValue('credential_id');
      if (!currentVal || !credentials.some((c) => c.id === currentVal)) {
        connectForm.setFieldValue('credential_id', credentials[0].id);
        setSelectedCredId(credentials[0].id);
      }
    } else if (rawCredentials !== undefined && rawCredentials.length === 0) {
      setTokenStatus('unknown');
      setShowTokenForm(true);
    }
  }, [credentials, rawCredentials, connectForm]);

  // Handle integration or credential fetch errors
  useEffect(() => {
    if (integrationsError) {
      setActionError(integrationsError instanceof Error ? integrationsError.message : 'Failed to load integrations');
    } else if (credentialsError) {
      setActionError(credentialsError instanceof Error ? credentialsError.message : 'Failed to load credentials');
    }
  }, [integrationsError, credentialsError]);

  // Query branches for newly selected repo
  const newRepoSlug = formRepoValue || '';
  const shouldFetchNewRepoBranches = Boolean(
    activeTeamId && activeCredId && newRepoSlug.includes('/')
  );

  const {
    data: queriedBranches,
    isLoading: loadingBranches,
    refetch: refetchFormBranches,
  } = useQuery({
    ...gitHubBranchesQueryOptions(activeTeamId || '', activeCredId, newRepoSlug),
    enabled: shouldFetchNewRepoBranches,
    retry: false,
  });

  const availableBranches = useMemo(() => queriedBranches || [], [queriedBranches]);

  useEffect(() => {
    if (queriedBranches && queriedBranches.length > 0) {
      connectForm.setFieldValue('base_branch', queriedBranches[0]);
    }
  }, [queriedBranches, connectForm]);

  // --- MUTATIONS ---
  const createIntegrationMutation = useMutation({
    mutationFn: ({ projectId, data }: { projectId: string; data: CreateIntegrationInput }) =>
      integrationApi.createProjectIntegration(projectId, data),
    onSuccess: (created) => {
      queryClient.setQueryData<IntegrationDTO[]>(
        ['integrations', activeProjectId],
        (old) => (old ? [...old, created] : [created])
      );
      queryClient.invalidateQueries({ queryKey: ['integrations', activeProjectId] });
      connectForm.reset();
      setActiveTab('repos');
      setActionSuccess(`Connected ${created.external_id} successfully!`);
    },
    onError: (err) => {
      setActionError(err instanceof Error ? err.message : 'Failed to connect repository');
    },
  });

  const updateIntegrationMutation = useMutation({
    mutationFn: ({
      projectId,
      integrationId,
      data,
    }: {
      projectId: string;
      integrationId: string;
      data: UpdateIntegrationInput;
    }) => integrationApi.updateProjectIntegration(projectId, integrationId, data),
    onSuccess: (updated) => {
      queryClient.setQueryData<IntegrationDTO[]>(
        ['integrations', activeProjectId],
        (old) => (old ? old.map((i) => (i.id === updated.id ? updated : i)) : [updated])
      );
      queryClient.invalidateQueries({ queryKey: ['integrations', activeProjectId] });
      setActionSuccess(`Updated ${updated.external_id} base branch to ${updated.config?.base_branch || 'main'}!`);
    },
    onError: (err) => {
      setActionError(err instanceof Error ? err.message : 'Failed to update base branch');
    },
    onSettled: () => {
      setUpdatingBranchIntegId(null);
    },
  });

  const deleteIntegrationMutation = useMutation({
    mutationFn: ({ projectId, integrationId }: { projectId: string; integrationId: string }) =>
      integrationApi.deleteProjectIntegration(projectId, integrationId),
    onSuccess: async (_, { integrationId }) => {
      queryClient.setQueryData<IntegrationDTO[]>(
        ['integrations', activeProjectId],
        (old) => (old ? old.filter((i) => i.id !== integrationId) : [])
      );
      setActionSuccess('Repository integration removed');
    },
    onError: (err) => {
      setActionError(err instanceof Error ? err.message : 'Failed to delete integration');
    },
  });

  const createCredentialMutation = useMutation({
    mutationFn: ({ teamId, data }: { teamId: string; data: CreateCredentialInput }) =>
      credentialApi.createCredential(teamId, data),
    onSuccess: async (savedCred) => {
      queryClient.setQueryData<CredentialDTO[]>(
        ['credentials', activeTeamId],
        (old) => (old ? [...old, savedCred] : [savedCred])
      );
      queryClient.invalidateQueries({ queryKey: ['credentials', activeTeamId] });
      connectForm.setFieldValue('credential_id', savedCred.id);
      setSelectedCredId(savedCred.id);
      setShowTokenForm(false);
      setEditingCred(null);
      setTokenSecret('');

      // Test token immediately
      if (activeTeamId) {
        try {
          const repos = await credentialApi.listGitHubRepositories(activeTeamId, savedCred.id);
          queryClient.setQueryData(
            gitHubReposQueryOptions(activeTeamId, savedCred.id).queryKey,
            repos
          );
          setTokenStatus('valid');
          setActionSuccess(`✓ Token saved and verified! Found ${repos.length} accessible repositories.`);
        } catch (testErr) {
          setTokenStatus('invalid');
          setActionError(testErr instanceof Error ? testErr.message : 'Token saved, but verification failed');
        }
      }
    },
    onError: (err) => {
      setTokenFormError(err instanceof Error ? err.message : 'Failed to save credential');
    },
  });

  const updateCredentialMutation = useMutation({
    mutationFn: ({
      teamId,
      credentialId,
      data,
    }: {
      teamId: string;
      credentialId: string;
      data: { label?: string; secret?: string };
    }) => credentialApi.updateCredential(teamId, credentialId, data),
    onSuccess: async (savedCred) => {
      queryClient.setQueryData<CredentialDTO[]>(
        ['credentials', activeTeamId],
        (old) => (old ? old.map((c) => (c.id === savedCred.id ? savedCred : c)) : [savedCred])
      );
      queryClient.invalidateQueries({ queryKey: ['credentials', activeTeamId] });
      connectForm.setFieldValue('credential_id', savedCred.id);
      setSelectedCredId(savedCred.id);
      setShowTokenForm(false);
      setEditingCred(null);
      setTokenSecret('');

      // Test token immediately
      if (activeTeamId) {
        try {
          const repos = await credentialApi.listGitHubRepositories(activeTeamId, savedCred.id);
          queryClient.setQueryData(
            gitHubReposQueryOptions(activeTeamId, savedCred.id).queryKey,
            repos
          );
          setTokenStatus('valid');
          setActionSuccess(`✓ Token saved and verified! Found ${repos.length} accessible repositories.`);
        } catch (testErr) {
          setTokenStatus('invalid');
          setActionError(testErr instanceof Error ? testErr.message : 'Token saved, but verification failed');
        }
      }
    },
    onError: (err) => {
      setTokenFormError(err instanceof Error ? err.message : 'Failed to save credential');
    },
  });

  const deleteCredentialMutation = useMutation({
    mutationFn: ({ teamId, credentialId }: { teamId: string; credentialId: string }) =>
      credentialApi.deleteCredential(teamId, credentialId),
    onSuccess: (_, { credentialId }) => {
      queryClient.setQueryData<CredentialDTO[]>(
        ['credentials', activeTeamId],
        (old) => (old ? old.filter((c) => c.id !== credentialId) : [])
      );
      queryClient.invalidateQueries({ queryKey: ['credentials', activeTeamId] });
      const remaining = credentials.filter((c) => c.id !== credentialId);
      if (remaining.length > 0) {
        connectForm.setFieldValue('credential_id', remaining[0].id);
        setSelectedCredId(remaining[0].id);
      } else {
        connectForm.setFieldValue('credential_id', '');
        setSelectedCredId('');
        setTokenStatus('unknown');
        setShowTokenForm(true);
      }
      setActionSuccess('Token deleted successfully');
    },
    onError: (err) => {
      setActionError(err instanceof Error ? err.message : 'Failed to delete token');
    },
  });

  const fetchBranchesForIntegration = useCallback(
    async (integ: IntegrationDTO) => {
      if (!activeTeamId || !integ.credential_id || !integ.external_id) return;
      setLoadingBranchesForInteg((prev) => ({ ...prev, [integ.id]: true }));
      try {
        const branches = await credentialApi.listGitHubBranches(
          activeTeamId,
          integ.credential_id,
          integ.external_id
        );
        setConnectedRepoBranches((prev) => ({ ...prev, [integ.id]: branches }));
        queryClient.setQueryData(
          gitHubBranchesQueryOptions(activeTeamId, integ.credential_id, integ.external_id).queryKey,
          branches
        );
      } catch (err) {
        console.warn('Failed to fetch branches for integration:', err);
      } finally {
        setLoadingBranchesForInteg((prev) => ({ ...prev, [integ.id]: false }));
      }
    },
    [activeTeamId, queryClient]
  );

  // Pre-fetch branches for connected integrations on open
  useEffect(() => {
    if (open && integrations.length > 0) {
      integrations.forEach((i) => {
        if (i.enabled && !connectedRepoBranches[i.id]) {
          fetchBranchesForIntegration(i);
        }
      });
    }
  }, [open, integrations, fetchBranchesForIntegration, connectedRepoBranches]);

  const handleUpdateIntegrationBranch = async (integ: IntegrationDTO, newBranch: string) => {
    if (!activeProjectId) return;
    setUpdatingBranchIntegId(integ.id);
    updateIntegrationMutation.mutate({
      projectId: activeProjectId,
      integrationId: integ.id,
      data: { base_branch: newBranch },
    });
  };

  const handleTestToken = async (credId: string) => {
    if (!activeTeamId) return;
    setTestingCredId(credId);
    setActionError(null);
    setActionSuccess(null);
    try {
      const repos = await credentialApi.listGitHubRepositories(activeTeamId, credId);
      queryClient.setQueryData(
        gitHubReposQueryOptions(activeTeamId, credId).queryKey,
        repos
      );
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

    setTokenFormError(null);
    setActionError(null);

    if (editingCred) {
      updateCredentialMutation.mutate({
        teamId: activeTeamId,
        credentialId: editingCred.id,
        data: {
          label: trimmedLabel,
          secret: trimmedSecret || undefined,
        },
      });
    } else {
      createCredentialMutation.mutate({
        teamId: activeTeamId,
        data: {
          provider: 'github',
          label: trimmedLabel,
          secret: trimmedSecret,
        },
      });
    }
  };

  const savingToken = createCredentialMutation.isPending || updateCredentialMutation.isPending;

  const handleDeleteToken = async (credId: string) => {
    if (!activeTeamId) return;
    if (!confirm('Are you sure you want to delete this GitHub token? Associated integrations may stop functioning.')) {
      return;
    }
    setActionError(null);
    deleteCredentialMutation.mutate({ teamId: activeTeamId, credentialId: credId });
  };

  const handleSelectRepository = (item: GitHubRepoDTO) => {
    connectForm.setFieldValue('repo', item.full_name);
    connectForm.setFieldValue('base_branch', item.default_branch || 'main');
    const credId = connectForm.getFieldValue('credential_id') || activeCredId;
    if (credId) {
      setSelectedCredId(credId);
    }
  };

  const filteredRepositories = useMemo(() => {
    let list = gitHubRepos;
    if (repoScopeFilter === 'connected') {
      const connectedSlugs = new Set(integrations.map((i) => i.external_id));
      list = list.filter((r) => connectedSlugs.has(r.full_name));
    } else if (repoScopeFilter === 'my') {
      const connectedOwner = integrations.length > 0 ? integrations[0].external_id.split('/')[0] : '';
      const firstOwner = gitHubRepos.length > 0 ? gitHubRepos[0].full_name.split('/')[0] : '';
      const owner = connectedOwner || firstOwner;
      if (owner) {
        list = list.filter((r) => r.full_name.startsWith(owner + '/'));
      }
    }

    const q = repoFilterQuery.trim().toLowerCase();
    if (!q) return list;
    return list.filter(
      (r) =>
        r.name.toLowerCase().includes(q) ||
        r.full_name.toLowerCase().includes(q) ||
        (r.description && r.description.toLowerCase().includes(q))
    );
  }, [gitHubRepos, repoFilterQuery, repoScopeFilter, integrations]);

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

    deleteIntegrationMutation.mutate({ projectId: activeProjectId, integrationId: id });
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
                  className="h-7 text-xs font-medium gap-1 cursor-pointer"
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

        {/* Tab Navigation: 3 Dedicated Tabs */}
        <Tabs
          value={activeTab}
          onValueChange={(val) => setActiveTab(val as 'repos' | 'connect' | 'pat')}
          className="w-full space-y-4 pt-1"
        >
          <TabsList className="grid grid-cols-1 sm:grid-cols-3 w-full max-w-2xl lg:max-w-3xl h-auto min-h-10 p-1 bg-muted/60 border border-border/50 rounded-xl gap-1">
            <TabsTrigger
              value="repos"
              className="text-xs px-3 py-2 flex items-center justify-center gap-2 cursor-pointer h-8 rounded-lg data-[state=active]:bg-card data-[state=active]:text-foreground data-[state=active]:shadow-sm font-medium transition-all"
              data-testid="tab-repositories"
            >
              <GitBranch className="size-3.5 shrink-0" />
              <span className="truncate">Connected Repositories</span>
              <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4 font-mono font-medium shrink-0">
                {integrations.length}
              </Badge>
            </TabsTrigger>

            <TabsTrigger
              value="connect"
              className="text-xs px-3 py-2 flex items-center justify-center gap-2 cursor-pointer h-8 rounded-lg data-[state=active]:bg-card data-[state=active]:text-foreground data-[state=active]:shadow-sm font-medium transition-all"
              data-testid="tab-connect"
            >
              <Plus className="size-3.5 shrink-0" />
              <span className="truncate">Connect Repository</span>
            </TabsTrigger>

            <TabsTrigger
              value="pat"
              className="text-xs px-3 py-2 flex items-center justify-center gap-2 cursor-pointer h-8 rounded-lg data-[state=active]:bg-card data-[state=active]:text-foreground data-[state=active]:shadow-sm font-medium transition-all"
              data-testid="tab-pat"
            >
              <KeyRound className="size-3.5 shrink-0" />
              <span className="truncate">GitHub Token (PAT)</span>
              {isTokenExpired ? (
                <Badge variant="destructive" className="text-[9px] px-1.5 py-0 h-4 font-mono font-medium shrink-0 animate-pulse">
                  Expired
                </Badge>
              ) : credentials.length > 0 ? (
                <Badge variant="outline" className="text-[9px] px-1.5 py-0 h-4 font-mono font-medium text-emerald-600 dark:text-emerald-400 border-emerald-500/30 shrink-0">
                  Active
                </Badge>
              ) : (
                <Badge variant="secondary" className="text-[9px] px-1.5 py-0 h-4 font-mono font-medium shrink-0 bg-muted-foreground/15 text-muted-foreground">
                  Setup Needed
                </Badge>
              )}
            </TabsTrigger>
          </TabsList>

          {/* ============================================================ */}
          {/* TAB 1: CONNECTED REPOSITORIES TABLE VIEW */}
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
                variant="default"
                onClick={() => setActiveTab('connect')}
                className="h-7 text-xs gap-1 cursor-pointer"
                data-testid="toggle-connect-repo-btn"
              >
                <Plus className="size-3" />
                <span>+ Connect Repository</span>
              </Button>
            </div>

            {/* Repositories Table / Empty State */}
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
                  Click "+ Connect Repository" to link a repo. Make sure your GitHub Personal Access Token is configured first.
                </p>
                <div className="flex items-center justify-center gap-2 pt-1">
                  <Button
                    size="xs"
                    variant="default"
                    onClick={() => setActiveTab('connect')}
                    className="text-xs gap-1"
                  >
                    <Plus className="size-3" />
                    <span>Connect Repository</span>
                  </Button>
                  {credentials.length === 0 && (
                    <Button
                      size="xs"
                      variant="outline"
                      onClick={() => setActiveTab('pat')}
                      className="text-xs gap-1.5"
                    >
                      <KeyRound className="size-3 text-primary" />
                      Configure GitHub Token (PAT)
                    </Button>
                  )}
                </div>
              </div>
            ) : (
              <div className="rounded-md border border-border bg-card overflow-hidden">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-[30%]">Repository</TableHead>
                      <TableHead className="w-[12%]">Status</TableHead>
                      <TableHead className="w-[28%]">Base Branch</TableHead>
                      <TableHead className="w-[12%]">Auto PR</TableHead>
                      <TableHead className="w-[18%] text-right">Actions</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {integrations.map((item) => {
                      const branches = connectedRepoBranches[item.id] || [];
                      const isFetchingBranches = loadingBranchesForInteg[item.id] || false;
                      const currentBranch = String(item.config?.base_branch || 'main');

                      return (
                        <TableRow
                          key={item.id}
                          className="hover:bg-muted/40 transition-colors"
                          data-testid={`integration-row-${item.id}`}
                        >
                          {/* Column 1: Repository */}
                          <TableCell className="font-medium">
                            <div className="flex items-center gap-2.5 min-w-0">
                              <div className="size-7 rounded-md bg-muted/60 flex items-center justify-center shrink-0">
                                <GitBranch className="size-3.5 text-primary" />
                              </div>
                              <div className="min-w-0 space-y-0.5">
                                <div className="flex items-center gap-1.5">
                                  <span className="font-semibold text-xs font-mono text-foreground truncate">
                                    {item.external_id}
                                  </span>
                                  {item.external_url && (
                                    <a
                                      href={item.external_url}
                                      target="_blank"
                                      rel="noopener noreferrer"
                                      className="text-muted-foreground hover:text-primary transition-colors inline-flex"
                                      title="Open repository on GitHub"
                                    >
                                      <ExternalLink className="size-3 shrink-0" />
                                    </a>
                                  )}
                                </div>
                                {Boolean(item.config?.filter_label) && (
                                  <p className="text-[10px] text-muted-foreground font-mono">
                                    Label: {String(item.config?.filter_label)}
                                  </p>
                                )}
                              </div>
                            </div>
                          </TableCell>

                          {/* Column 2: Status */}
                          <TableCell>
                            {item.enabled ? (
                              <Badge className="bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border border-emerald-500/30 text-[10px] font-mono py-0 h-4">
                                Active
                              </Badge>
                            ) : (
                              <Badge variant="outline" className="text-[10px] font-mono py-0 h-4 text-muted-foreground">
                                Disabled
                              </Badge>
                            )}
                          </TableCell>

                          {/* Column 3: Base Branch (shadcn Command Branch Picker) */}
                          <TableCell>
                            <div className="flex items-center gap-1.5">
                              <BranchCommandPicker
                                value={currentBranch}
                                branches={branches}
                                isLoading={isFetchingBranches}
                                disabled={updatingBranchIntegId === item.id}
                                onSelect={(b) => handleUpdateIntegrationBranch(item, b)}
                                onRefresh={() => fetchBranchesForIntegration(item)}
                                testId={`branch-select-${item.id}`}
                              />
                            </div>
                          </TableCell>

                          {/* Column 4: Auto PR */}
                          <TableCell>
                            {item.config?.auto_pr !== false ? (
                              <Badge variant="secondary" className="text-[10px] font-mono py-0 h-4">
                                Enabled
                              </Badge>
                            ) : (
                              <Badge variant="outline" className="text-[10px] font-mono py-0 h-4 text-muted-foreground">
                                Off
                              </Badge>
                            )}
                          </TableCell>

                          {/* Column 5: Actions */}
                          <TableCell className="text-right">
                            <div className="flex items-center justify-end gap-1">
                              <Button
                                size="icon-xs"
                                variant="ghost"
                                onClick={() => fetchBranchesForIntegration(item)}
                                disabled={isFetchingBranches}
                                className="text-muted-foreground hover:text-foreground cursor-pointer"
                                title="Refresh branches from GitHub"
                                data-testid={`fetch-branches-btn-${item.id}`}
                              >
                                <RefreshCw className={cn('size-3', isFetchingBranches && 'animate-spin')} />
                              </Button>

                              <Button
                                size="icon-xs"
                                variant="ghost"
                                onClick={() => handleDeleteIntegration(item.id)}
                                className="text-muted-foreground hover:text-destructive hover:bg-destructive/10 cursor-pointer"
                                title="Disconnect repository"
                                data-testid={`delete-integration-${item.id}`}
                              >
                                <Trash2 className="size-3.5" />
                              </Button>
                            </div>
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </div>
            )}
          </TabsContent>

          {/* ============================================================ */}
          {/* TAB 2: CONNECT REPOSITORY VIEW */}
          {/* ============================================================ */}
          <TabsContent value="connect" className="space-y-4 mt-2">
            <div
              className="p-4 rounded-lg border border-border bg-card shadow-xs space-y-4"
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
                  className="text-[11px] text-primary hover:underline font-medium cursor-pointer"
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
                        {credentials.find((c) => c.id === (connectForm.getFieldValue('credential_id') || activeCredId))?.label ||
                          credentials[0].label}
                      </b>
                    </span>
                  </div>
                  <button
                    type="button"
                    onClick={() => setActiveTab('pat')}
                    className="text-[11px] text-primary hover:underline font-medium cursor-pointer"
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
                    className="h-6 text-[11px] cursor-pointer"
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
                              onChange={(e) => {
                                const val = e.target.value;
                                repoField.handleChange(val);
                              }}
                              className="font-mono text-xs h-8"
                              data-testid="repo-input"
                            />
                          </div>
                        ) : (
                          <div className="space-y-2">
                            {/* Scope Filter Options */}
                            <div className="flex items-center gap-1.5 flex-wrap">
                              <button
                                type="button"
                                onClick={() => setRepoScopeFilter('connected')}
                                className={`px-2.5 py-1 rounded-md text-[11px] font-medium transition-colors cursor-pointer ${
                                  repoScopeFilter === 'connected'
                                    ? 'bg-primary text-primary-foreground shadow-xs'
                                    : 'bg-muted/50 text-muted-foreground hover:bg-muted hover:text-foreground'
                                }`}
                                data-testid="scope-connected-repos"
                              >
                                Connected in Project ({integrations.length})
                              </button>
                              <button
                                type="button"
                                onClick={() => setRepoScopeFilter('my')}
                                className={`px-2.5 py-1 rounded-md text-[11px] font-medium transition-colors cursor-pointer ${
                                  repoScopeFilter === 'my'
                                    ? 'bg-primary text-primary-foreground shadow-xs'
                                    : 'bg-muted/50 text-muted-foreground hover:bg-muted hover:text-foreground'
                                }`}
                                data-testid="scope-my-repos"
                              >
                                My Account Repos
                              </button>
                              <button
                                type="button"
                                onClick={() => setRepoScopeFilter('all')}
                                className={`px-2.5 py-1 rounded-md text-[11px] font-medium transition-colors cursor-pointer ${
                                  repoScopeFilter === 'all'
                                    ? 'bg-primary text-primary-foreground shadow-xs'
                                    : 'bg-muted/50 text-muted-foreground hover:bg-muted hover:text-foreground'
                                }`}
                                data-testid="scope-all-repos"
                              >
                                All Accessible ({gitHubRepos.length})
                              </button>
                            </div>

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
                                  className="text-[11px] text-primary hover:underline font-medium cursor-pointer"
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
                                    const isAlreadyConnected = integrations.some(
                                      (i) => i.external_id === item.full_name
                                    );

                                    return (
                                      <button
                                        key={item.id}
                                        type="button"
                                        onClick={() => handleSelectRepository(item)}
                                        className={`w-full text-left p-2.5 transition-colors flex items-center justify-between gap-3 text-xs cursor-pointer ${
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
                                            {isAlreadyConnected && (
                                              <Badge className="bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border border-emerald-500/30 text-[9px] font-mono py-0 h-4 ml-1">
                                                Connected
                                              </Badge>
                                            )}
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

                            {/* Selected repository input */}
                            <div className="pt-1">
                              <label htmlFor="repo-input" className="text-[11px] font-medium text-foreground block mb-1">
                                Selected Repository *
                              </label>
                              <Input
                                id="repo-input"
                                placeholder="owner/repo (or click repository in list above)"
                                value={repoField.state.value}
                                onBlur={repoField.handleBlur}
                                onChange={(e) => {
                                  const val = e.target.value;
                                  repoField.handleChange(val);
                                }}
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
                        className="h-7 w-7 shrink-0 cursor-pointer"
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
                              className="h-5 text-[10px] text-muted-foreground gap-1 px-1 cursor-pointer"
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
                        <div className="flex items-center justify-between">
                          <label htmlFor="base-branch-input" className="text-xs font-medium text-foreground flex items-center gap-1">
                            <GitBranch className="size-3 text-primary" />
                            <span>Base Branch *</span>
                          </label>
                          {loadingBranches ? (
                            <span className="text-[10px] text-muted-foreground flex items-center gap-1">
                              <RefreshCw className="size-2.5 animate-spin text-primary" />
                              <span>Fetching branches...</span>
                            </span>
                          ) : availableBranches.length > 0 ? (
                            <span className="text-[10px] text-muted-foreground font-mono">
                              {availableBranches.length} branches found
                            </span>
                          ) : null}
                        </div>

                        {availableBranches.length > 0 ? (
                          <div className="space-y-1">
                            <BranchCommandPicker
                              value={field.state.value || 'main'}
                              branches={availableBranches}
                              isLoading={loadingBranches}
                              onSelect={(b) => field.handleChange(b)}
                              onRefresh={() => refetchFormBranches()}
                              testId="base-branch-picker"
                              triggerClassName="w-full h-8"
                            />
                            {/* Hidden/accessible fallback for compatibility with existing tests */}
                            <input
                              type="hidden"
                              id="base-branch-input"
                              data-testid="base-branch-input"
                              value={field.state.value || ''}
                              readOnly
                            />
                          </div>
                        ) : (
                          <Input
                            id="base-branch-input"
                            placeholder="main"
                            value={field.state.value}
                            onBlur={field.handleBlur}
                            onChange={(e) => field.handleChange(e.target.value)}
                            className="text-xs h-8 font-mono"
                            data-testid="base-branch-input"
                          />
                        )}
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
                        className="size-4 rounded border-input text-primary focus:ring-ring cursor-pointer"
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
                    onClick={() => setActiveTab('repos')}
                    className="text-xs cursor-pointer"
                  >
                    Cancel
                  </Button>
                  <connectForm.Subscribe
                    selector={(state) => [state.isSubmitting]}
                    children={([isSubmitting]) => (
                      <Button
                        type="submit"
                        size="sm"
                        disabled={isSubmitting || createIntegrationMutation.isPending}
                        className="text-xs font-medium cursor-pointer"
                        data-testid="connect-submit-btn"
                      >
                        {isSubmitting || createIntegrationMutation.isPending ? 'Connecting...' : 'Connect Repository'}
                      </Button>
                    )}
                  />
                </div>
              </form>
            </div>
          </TabsContent>

          {/* ============================================================ */}
          {/* TAB 3: GITHUB AUTHENTICATION & PAT MANAGEMENT */}
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
                  className="h-7 text-xs gap-1 cursor-pointer"
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
                  const isCurrentTarget = (connectForm.getFieldValue('credential_id') || activeCredId) === c.id;
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
                            className="h-7 text-xs gap-1 cursor-pointer"
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
                            className="h-7 text-xs gap-1 cursor-pointer"
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
                            className="text-muted-foreground hover:text-destructive hover:bg-destructive/10 cursor-pointer"
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
                      className="h-6 text-xs cursor-pointer"
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
                        className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground cursor-pointer"
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
                      className="text-xs cursor-pointer"
                    >
                      Cancel
                    </Button>
                  )}
                  <Button
                    type="submit"
                    size="sm"
                    disabled={savingToken}
                    className="text-xs font-medium gap-1.5 cursor-pointer"
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
