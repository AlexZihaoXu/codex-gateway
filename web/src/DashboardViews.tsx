import {
  Alert,
  AlertDialog,
  Button,
  Card,
  Chip,
  Form,
  Input,
  Label,
  ListBox,
  Modal,
  NumberField,
  ProgressBar,
  Select,
  Spinner,
  Surface,
  Table,
  TextField,
  Typography,
} from '@heroui/react';
import {
  AlertTriangle,
  Check,
  Clipboard,
  KeyRound,
  Minus,
  Pencil,
  Plus,
  ShieldAlert,
  Trash2,
} from 'lucide-react';
import { type FormEvent, useEffect, useMemo, useState } from 'react';

import { CodexAccountCard } from './CodexAccountCard';
import {
  type APIKey,
  type IntervalUnit,
  type Project,
  type UsageLimit,
  type UsageSummary,
  createAPIKey,
  createProject,
  deleteProject,
  listAPIKeys,
  listProjects,
  readUsageSummary,
  revokeAPIKey,
  updateProject,
} from './project-api';

export function ProjectsView() {
  const [projects, setProjects] = useState<Project[]>();
  const [error, setError] = useState<string>();
  const [isOpen, setIsOpen] = useState(false);
  const [name, setName] = useState('');
  const [limits, setLimits] = useState<UsageLimit[]>([]);
  const [saving, setSaving] = useState(false);
  const [editCandidate, setEditCandidate] = useState<Project>();
  const [editName, setEditName] = useState('');
  const [editLimits, setEditLimits] = useState<UsageLimit[]>([]);
  const [deleteCandidate, setDeleteCandidate] = useState<Project>();

  useEffect(() => {
    const controller = new AbortController();
    listProjects(controller.signal)
      .then(setProjects)
      .catch((cause: unknown) => {
        if (!controller.signal.aborted) setError(errorMessage(cause, 'Unable to load projects.'));
      });
    return () => controller.abort();
  }, []);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSaving(true);
    setError(undefined);
    try {
      const project = await createProject(name.trim(), limits);
      setProjects((current) => [...(current ?? []), project]);
      setIsOpen(false);
      setName('');
      setLimits([]);
    } catch (cause) {
      setError(errorMessage(cause, 'Unable to create the project.'));
    } finally {
      setSaving(false);
    }
  }

  function beginEdit(project: Project) {
    setEditCandidate(project);
    setEditName(project.name);
    setEditLimits(project.limits.map((limit) => ({ ...limit })));
  }

  async function submitEdit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!editCandidate) return;
    setSaving(true);
    setError(undefined);
    try {
      const updated = await updateProject(editCandidate.id, editName.trim(), editLimits);
      setProjects((current) =>
        current?.map((project) => (project.id === updated.id ? updated : project)),
      );
      setEditCandidate(undefined);
    } catch (cause) {
      setError(errorMessage(cause, 'Unable to update the project.'));
    } finally {
      setSaving(false);
    }
  }

  async function removeProject(project: Project) {
    setError(undefined);
    try {
      await deleteProject(project.id);
      setProjects((current) => current?.filter((item) => item.id !== project.id));
      setDeleteCandidate(undefined);
    } catch (cause) {
      setError(errorMessage(cause, 'Unable to delete the project.'));
    }
  }

  return (
    <section aria-labelledby="projects-heading" className="flex w-full min-w-0 flex-col gap-6 py-8">
      <ViewHeader
        action={<Button onPress={() => setIsOpen(true)}>Create project</Button>}
        description="Group API keys, usage, and fixed-window safeguards by application."
        title="Projects"
      />
      {error ? <ErrorAlert message={error} /> : null}
      {projects === undefined && !error ? (
        <LoadingState label="Loading projects" />
      ) : projects?.length === 0 ? (
        <EmptyCard
          description="Create a project before issuing an API key."
          title="No projects yet"
        />
      ) : projects ? (
        <Table>
          <Table.ScrollContainer>
            <Table.Content aria-label="Projects">
              <Table.Header>
                <Table.Column isRowHeader>Name</Table.Column>
                <Table.Column>Usage limits</Table.Column>
                <Table.Column>Created</Table.Column>
                <Table.Column>Actions</Table.Column>
              </Table.Header>
              <Table.Body>
                {projects.map((project) => (
                  <Table.Row id={project.id} key={project.id}>
                    <Table.Cell>{project.name}</Table.Cell>
                    <Table.Cell>
                      {project.limits.length === 0
                        ? 'Unlimited'
                        : project.limits.map(formatUsageLimit).join(', ')}
                    </Table.Cell>
                    <Table.Cell>{formatDate(project.createdAt)}</Table.Cell>
                    <Table.Cell>
                      <div className="flex items-center gap-2">
                        <Button
                          aria-label={`Edit ${project.name}`}
                          size="sm"
                          variant="ghost"
                          onPress={() => beginEdit(project)}
                        >
                          <Pencil aria-hidden="true" size={15} />
                          Edit
                        </Button>
                        <Button
                          aria-label={`Delete ${project.name}`}
                          size="sm"
                          variant="danger-soft"
                          onPress={() => setDeleteCandidate(project)}
                        >
                          <Trash2 aria-hidden="true" size={15} />
                          Delete
                        </Button>
                      </div>
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table.Content>
          </Table.ScrollContainer>
        </Table>
      ) : null}

      <Modal.Backdrop isOpen={isOpen} onOpenChange={setIsOpen}>
        <Modal.Container placement="center" size="md">
          <Modal.Dialog>
            <Form onSubmit={submit}>
              <Modal.Header>
                <Modal.Heading>Create project</Modal.Heading>
              </Modal.Header>
              <Modal.Body>
                <div className="flex flex-col gap-5">
                  <TextField fullWidth isRequired value={name} onChange={setName}>
                    <Label>Project name</Label>
                    <Input placeholder="OpenCode" />
                  </TextField>
                  <UsageLimitEditor limits={limits} onChange={setLimits} />
                </div>
              </Modal.Body>
              <Modal.Footer>
                <Button variant="secondary" onPress={() => setIsOpen(false)}>
                  Cancel
                </Button>
                <Button
                  isDisabled={saving || name.trim().length === 0 || !usageLimitsAreValid(limits)}
                  type="submit"
                >
                  {saving ? <Spinner color="current" size="sm" /> : null}
                  Create project
                </Button>
              </Modal.Footer>
            </Form>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>

      <Modal.Backdrop
        isOpen={editCandidate !== undefined}
        onOpenChange={(open) => {
          if (!open) setEditCandidate(undefined);
        }}
      >
        <Modal.Container placement="center" size="md">
          <Modal.Dialog>
            <Form onSubmit={submitEdit}>
              <Modal.Header>
                <Modal.Heading>Edit project</Modal.Heading>
              </Modal.Header>
              <Modal.Body>
                <div className="flex flex-col gap-5">
                  <TextField fullWidth isRequired value={editName} onChange={setEditName}>
                    <Label>Project name</Label>
                    <Input />
                  </TextField>
                  <UsageLimitEditor limits={editLimits} onChange={setEditLimits} />
                </div>
              </Modal.Body>
              <Modal.Footer>
                <Button variant="secondary" onPress={() => setEditCandidate(undefined)}>
                  Cancel
                </Button>
                <Button
                  isDisabled={
                    saving || editName.trim().length === 0 || !usageLimitsAreValid(editLimits)
                  }
                  type="submit"
                >
                  {saving ? <Spinner color="current" size="sm" /> : null}
                  Save changes
                </Button>
              </Modal.Footer>
            </Form>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>

      <AlertDialog.Backdrop
        isOpen={deleteCandidate !== undefined}
        onOpenChange={(open) => {
          if (!open) setDeleteCandidate(undefined);
        }}
      >
        <AlertDialog.Container placement="center" size="sm">
          <AlertDialog.Dialog>
            <AlertDialog.Header>
              <AlertDialog.Icon status="danger">
                <ShieldAlert aria-hidden="true" size={20} />
              </AlertDialog.Icon>
              <AlertDialog.Heading>Delete {deleteCandidate?.name}?</AlertDialog.Heading>
            </AlertDialog.Header>
            <AlertDialog.Body>
              Deletes this project, all of its API keys, and its recorded usage. This cannot be
              undone.
            </AlertDialog.Body>
            <AlertDialog.Footer>
              <Button variant="secondary" onPress={() => setDeleteCandidate(undefined)}>
                Cancel
              </Button>
              <Button
                variant="danger"
                onPress={() => {
                  if (deleteCandidate) void removeProject(deleteCandidate);
                }}
              >
                Delete project
              </Button>
            </AlertDialog.Footer>
          </AlertDialog.Dialog>
        </AlertDialog.Container>
      </AlertDialog.Backdrop>
    </section>
  );
}

export function APIKeysView() {
  const [projects, setProjects] = useState<Project[]>();
  const [keys, setKeys] = useState<APIKey[]>();
  const [error, setError] = useState<string>();
  const [isOpen, setIsOpen] = useState(false);
  const [name, setName] = useState('');
  const [projectID, setProjectID] = useState('');
  const [secret, setSecret] = useState<string>();
  const [copyError, setCopyError] = useState<string>();
  const [copied, setCopied] = useState(false);
  const [saving, setSaving] = useState(false);
  const [revokeCandidate, setRevokeCandidate] = useState<APIKey>();

  useEffect(() => {
    const controller = new AbortController();
    Promise.all([listProjects(controller.signal), listAPIKeys(controller.signal)])
      .then(([projectItems, keyItems]) => {
        setProjects(projectItems);
        setKeys(keyItems);
        const firstProject = projectItems[0];
        if (firstProject) setProjectID(firstProject.id);
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted) setError(errorMessage(cause, 'Unable to load API keys.'));
      });
    return () => controller.abort();
  }, []);

  const projectNames = useMemo(
    () => new Map((projects ?? []).map((project) => [project.id, project.name])),
    [projects],
  );

  function closeCreation() {
    setIsOpen(false);
    setSecret(undefined);
    setCopied(false);
    setCopyError(undefined);
    setName('');
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSaving(true);
    setError(undefined);
    try {
      const created = await createAPIKey(projectID, name.trim());
      setKeys((current) => [...(current ?? []), created.apiKey]);
      setSecret(created.secret);
    } catch (cause) {
      setError(errorMessage(cause, 'Unable to create the API key.'));
    } finally {
      setSaving(false);
    }
  }

  async function revoke(key: APIKey) {
    setError(undefined);
    try {
      await revokeAPIKey(key.id);
      setKeys((current) => current?.filter((item) => item.id !== key.id));
      setRevokeCandidate(undefined);
    } catch (cause) {
      setError(errorMessage(cause, 'Unable to revoke the API key.'));
    }
  }

  async function copySecret() {
    if (!secret) return;
    setCopyError(undefined);
    try {
      await navigator.clipboard.writeText(secret);
      setCopied(true);
    } catch (cause) {
      setCopyError(errorMessage(cause, 'Unable to copy the API key.'));
    }
  }

  const loading = projects === undefined || keys === undefined;

  return (
    <section aria-labelledby="api-keys-heading" className="flex w-full min-w-0 flex-col gap-6 py-8">
      <ViewHeader
        action={
          <Button isDisabled={!projects?.length} onPress={() => setIsOpen(true)}>
            Create API key
          </Button>
        }
        description="Issue scoped credentials without exposing stored key material."
        title="API keys"
      />
      {error ? <ErrorAlert message={error} /> : null}
      {loading && !error ? (
        <LoadingState label="Loading API keys" />
      ) : projects?.length === 0 ? (
        <EmptyCard
          description="Create a project first, then issue its key."
          title="No project yet"
        />
      ) : keys?.length === 0 ? (
        <EmptyCard
          description="Keys are displayed once at creation and stored only as hashes."
          title="No API keys yet"
        />
      ) : keys ? (
        <Table>
          <Table.ScrollContainer>
            <Table.Content aria-label="API keys">
              <Table.Header>
                <Table.Column isRowHeader>Name</Table.Column>
                <Table.Column>Project</Table.Column>
                <Table.Column>Key prefix</Table.Column>
                <Table.Column>Last used</Table.Column>
                <Table.Column>Status</Table.Column>
                <Table.Column>Actions</Table.Column>
              </Table.Header>
              <Table.Body>
                {keys.map((key) => (
                  <Table.Row id={key.id} key={key.id}>
                    <Table.Cell>{key.name}</Table.Cell>
                    <Table.Cell>{projectNames.get(key.projectID) ?? 'Unknown project'}</Table.Cell>
                    <Table.Cell>{key.prefix}...</Table.Cell>
                    <Table.Cell>{key.lastUsedAt ? formatDate(key.lastUsedAt) : 'Never'}</Table.Cell>
                    <Table.Cell>
                      <Chip color={key.revokedAt ? 'default' : 'success'} size="sm" variant="soft">
                        {key.revokedAt ? 'Revoked' : 'Active'}
                      </Chip>
                    </Table.Cell>
                    <Table.Cell>
                      {key.revokedAt ? (
                        <Typography color="muted" type="body-sm">
                          -
                        </Typography>
                      ) : (
                        <Button
                          size="sm"
                          variant="danger-soft"
                          onPress={() => setRevokeCandidate(key)}
                        >
                          Revoke
                        </Button>
                      )}
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table.Content>
          </Table.ScrollContainer>
        </Table>
      ) : null}

      <Modal.Backdrop
        isOpen={isOpen}
        onOpenChange={(open) => {
          if (!open) closeCreation();
        }}
      >
        <Modal.Container placement="center" size="sm">
          <Modal.Dialog>
            {secret ? (
              <>
                <Modal.Header>
                  <Modal.Icon>
                    <KeyRound aria-hidden="true" size={20} />
                  </Modal.Icon>
                  <Modal.Heading>Save your API key</Modal.Heading>
                </Modal.Header>
                <Modal.Body>
                  <div className="flex flex-col gap-4">
                    <Alert status="warning">
                      <Alert.Indicator />
                      <Alert.Content>
                        <Alert.Title>Shown once</Alert.Title>
                        <Alert.Description>
                          Copy this key now. Codex Gateway cannot reveal it again.
                        </Alert.Description>
                      </Alert.Content>
                    </Alert>
                    <Surface variant="secondary">
                      <div className="break-all p-4">
                        <Typography type="code">{secret}</Typography>
                      </div>
                    </Surface>
                    {copyError ? <ErrorAlert message={copyError} /> : null}
                  </div>
                </Modal.Body>
                <Modal.Footer>
                  <Button variant="secondary" onPress={() => void copySecret()}>
                    {copied ? (
                      <Check aria-hidden="true" size={16} />
                    ) : (
                      <Clipboard aria-hidden="true" size={16} />
                    )}
                    {copied ? 'Copied' : 'Copy key'}
                  </Button>
                  <Button onPress={closeCreation}>Done</Button>
                </Modal.Footer>
              </>
            ) : (
              <Form onSubmit={submit}>
                <Modal.Header>
                  <Modal.Heading>Create API key</Modal.Heading>
                </Modal.Header>
                <Modal.Body>
                  <div className="flex flex-col gap-5">
                    <Select
                      fullWidth
                      isRequired
                      selectedKey={projectID}
                      onSelectionChange={(key) => setProjectID(String(key))}
                    >
                      <Label>Project</Label>
                      <Select.Trigger>
                        <Select.Value />
                        <Select.Indicator />
                      </Select.Trigger>
                      <Select.Popover>
                        <ListBox>
                          {(projects ?? []).map((project) => (
                            <ListBox.Item id={project.id} key={project.id} textValue={project.name}>
                              {project.name}
                              <ListBox.ItemIndicator />
                            </ListBox.Item>
                          ))}
                        </ListBox>
                      </Select.Popover>
                    </Select>
                    <TextField fullWidth isRequired value={name} onChange={setName}>
                      <Label>Key name</Label>
                      <Input placeholder="Development" />
                    </TextField>
                  </div>
                </Modal.Body>
                <Modal.Footer>
                  <Button variant="secondary" onPress={closeCreation}>
                    Cancel
                  </Button>
                  <Button
                    isDisabled={saving || projectID.length === 0 || name.trim().length === 0}
                    type="submit"
                  >
                    {saving ? <Spinner color="current" size="sm" /> : null}
                    Create API key
                  </Button>
                </Modal.Footer>
              </Form>
            )}
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>

      <AlertDialog.Backdrop
        isOpen={revokeCandidate !== undefined}
        onOpenChange={(open) => {
          if (!open) setRevokeCandidate(undefined);
        }}
      >
        <AlertDialog.Container placement="center" size="sm">
          <AlertDialog.Dialog>
            <AlertDialog.Header>
              <AlertDialog.Icon status="danger">
                <ShieldAlert aria-hidden="true" size={20} />
              </AlertDialog.Icon>
              <AlertDialog.Heading>Revoke {revokeCandidate?.name}?</AlertDialog.Heading>
            </AlertDialog.Header>
            <AlertDialog.Body>
              Requests using this key will stop working immediately.
            </AlertDialog.Body>
            <AlertDialog.Footer>
              <Button variant="secondary" onPress={() => setRevokeCandidate(undefined)}>
                Cancel
              </Button>
              <Button
                variant="danger"
                onPress={() => {
                  if (revokeCandidate) void revoke(revokeCandidate);
                }}
              >
                Revoke key
              </Button>
            </AlertDialog.Footer>
          </AlertDialog.Dialog>
        </AlertDialog.Container>
      </AlertDialog.Backdrop>
    </section>
  );
}

export function UsageView() {
  const [usage, setUsage] = useState<UsageSummary>();
  const [error, setError] = useState<string>();

  useEffect(() => {
    const controller = new AbortController();
    readUsageSummary(controller.signal)
      .then(setUsage)
      .catch((cause: unknown) => {
        if (!controller.signal.aborted) setError(errorMessage(cause, 'Unable to load usage.'));
      });
    return () => controller.abort();
  }, []);

  return (
    <section aria-labelledby="usage-heading" className="flex w-full min-w-0 flex-col gap-6 py-8">
      <ViewHeader
        description="Current UTC-month request and token totals, grouped by project and API key."
        title="Usage"
      />
      {error ? <ErrorAlert message={error} /> : null}
      {!usage && !error ? <LoadingState label="Loading usage" /> : null}
      {usage ? (
        <>
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <MetricCard label="Requests" value={usage.requests} />
            <MetricCard label="Input tokens" value={usage.inputTokens} />
            <MetricCard label="Output tokens" value={usage.outputTokens} />
            <MetricCard label="Total tokens" value={usage.totalTokens} />
          </div>
          {usage.projects.length === 0 ? (
            <EmptyCard
              description="Usage appears after a project sends its first request."
              title="No usage this month"
            />
          ) : (
            <div className="flex flex-col gap-4">
              {usage.projects.map((project) => (
                <Card key={project.projectID}>
                  <Card.Header>
                    <div className="flex w-full items-start justify-between gap-4">
                      <div>
                        <Card.Title>{project.name}</Card.Title>
                        <Card.Description>
                          {formatNumber(project.requests)} requests
                        </Card.Description>
                      </div>
                      <Chip size="sm" variant="soft">
                        {formatNumber(project.totalTokens)} tokens
                      </Chip>
                    </div>
                  </Card.Header>
                  {project.limits.length > 0 ? (
                    <Card.Content>
                      <div className="flex flex-col gap-5">
                        {project.limits.map((limit) => (
                          <div
                            className="flex flex-col gap-2"
                            key={`${limit.intervalUnit}-${limit.intervalCount}`}
                          >
                            <div className="flex flex-wrap items-baseline justify-between gap-2">
                              <Typography weight="medium">
                                {formatNumber(limit.totalTokens)} / {formatNumber(limit.tokenLimit)}{' '}
                                tokens every {formatInterval(limit)}
                              </Typography>
                              <Typography color="muted" type="body-xs">
                                Resets {formatDateTime(limit.periodEnd)}
                              </Typography>
                            </div>
                            <ProgressBar
                              aria-label={`${project.name} ${formatIntervalAdjective(limit)} token usage`}
                              maxValue={100}
                              value={Math.min(100, (limit.totalTokens / limit.tokenLimit) * 100)}
                            >
                              <ProgressBar.Track>
                                <ProgressBar.Fill />
                              </ProgressBar.Track>
                            </ProgressBar>
                          </div>
                        ))}
                      </div>
                    </Card.Content>
                  ) : null}
                  <Card.Footer>
                    <Typography color="muted" type="body-xs">
                      Current month: {formatNumber(project.inputTokens)} input ·{' '}
                      {formatNumber(project.cachedInputTokens)} cached ·{' '}
                      {formatNumber(project.outputTokens)} output ·{' '}
                      {formatNumber(project.reasoningOutputTokens)} reasoning
                    </Typography>
                  </Card.Footer>
                </Card>
              ))}
            </div>
          )}
          {usage.apiKeys.length > 0 ? (
            <section aria-labelledby="api-key-usage-heading" className="flex flex-col gap-3">
              <div className="flex flex-col gap-1">
                <Typography id="api-key-usage-heading" type="h4">
                  Usage by API key
                </Typography>
                <Typography color="muted" type="body-sm">
                  Token details identify which scoped credential generated each project total.
                </Typography>
              </div>
              <Table>
                <Table.ScrollContainer>
                  <Table.Content aria-label="API key usage">
                    <Table.Header>
                      <Table.Column isRowHeader>Name</Table.Column>
                      <Table.Column>Key prefix</Table.Column>
                      <Table.Column>Project</Table.Column>
                      <Table.Column>Requests</Table.Column>
                      <Table.Column>Input</Table.Column>
                      <Table.Column>Cached input</Table.Column>
                      <Table.Column>Output</Table.Column>
                      <Table.Column>Reasoning output</Table.Column>
                      <Table.Column>Total</Table.Column>
                    </Table.Header>
                    <Table.Body>
                      {usage.apiKeys.map((key) => (
                        <Table.Row id={key.apiKeyID} key={key.apiKeyID}>
                          <Table.Cell>{key.name}</Table.Cell>
                          <Table.Cell>{key.prefix}...</Table.Cell>
                          <Table.Cell>
                            {usage.projects.find((project) => project.projectID === key.projectID)
                              ?.name ?? 'Unknown project'}
                          </Table.Cell>
                          <Table.Cell>{formatNumber(key.requests)}</Table.Cell>
                          <Table.Cell>{formatNumber(key.inputTokens)}</Table.Cell>
                          <Table.Cell>{formatNumber(key.cachedInputTokens)}</Table.Cell>
                          <Table.Cell>{formatNumber(key.outputTokens)}</Table.Cell>
                          <Table.Cell>{formatNumber(key.reasoningOutputTokens)}</Table.Cell>
                          <Table.Cell>{formatNumber(key.totalTokens)}</Table.Cell>
                        </Table.Row>
                      ))}
                    </Table.Body>
                  </Table.Content>
                </Table.ScrollContainer>
              </Table>
            </section>
          ) : null}
        </>
      ) : null}
    </section>
  );
}

export function SettingsView() {
  return (
    <section aria-labelledby="settings-heading" className="flex w-full min-w-0 flex-col gap-6 py-8">
      <ViewHeader
        description="Account connectivity and local gateway security posture."
        title="Settings"
      />
      <div className="grid gap-4 xl:grid-cols-2">
        <CodexAccountCard allowDisconnect />
        <Card className="h-full">
          <Card.Header>
            <div className="flex w-full items-start justify-between gap-4">
              <div>
                <Card.Title>Network access</Card.Title>
                <Card.Description>
                  Codex Gateway is restricted to this machine by default.
                </Card.Description>
              </div>
              <Chip color="success" size="sm" variant="soft">
                Loopback only
              </Chip>
            </div>
          </Card.Header>
          <Card.Content>
            <Typography color="muted" type="body-sm">
              Only applications running on this computer can reach the gateway.
            </Typography>
          </Card.Content>
          <Card.Footer className="mt-auto">
            <div className="flex w-full items-center justify-between gap-4">
              <Typography color="muted" type="body-xs">
                Listening address
              </Typography>
              <Typography type="code">127.0.0.1:11435</Typography>
            </div>
          </Card.Footer>
        </Card>
      </div>
    </section>
  );
}

interface ViewHeaderProps {
  action?: React.ReactNode;
  description: string;
  title: string;
}

function ViewHeader({ action, description, title }: ViewHeaderProps) {
  const id = `${title.toLowerCase().replaceAll(' ', '-')}-heading`;
  return (
    <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
      <div className="flex flex-col gap-1">
        <Typography id={id} type="h2">
          {title}
        </Typography>
        <Typography color="muted">{description}</Typography>
      </div>
      {action}
    </div>
  );
}

function MetricCard({ label, value }: { label: string; value: number }) {
  return (
    <Card>
      <Card.Header>
        <Card.Description>{label}</Card.Description>
        <Card.Title>{formatNumber(value)}</Card.Title>
      </Card.Header>
    </Card>
  );
}

function UsageLimitEditor({
  limits,
  onChange,
}: {
  limits: UsageLimit[];
  onChange: (limits: UsageLimit[]) => void;
}) {
  function update(index: number, patch: Partial<UsageLimit>) {
    onChange(
      limits.map((limit, position) => (position === index ? { ...limit, ...patch } : limit)),
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <Typography type="h5">Usage limits</Typography>
          <Typography color="muted" type="body-sm">
            All configured fixed UTC windows are enforced.
          </Typography>
        </div>
        <Button
          isDisabled={limits.length >= 8}
          size="sm"
          variant="secondary"
          onPress={() =>
            onChange([...limits, { intervalCount: 1, intervalUnit: 'hour', tokenLimit: 100_000 }])
          }
        >
          <Plus aria-hidden="true" size={15} />
          Add usage limit
        </Button>
      </div>
      {limits.length === 0 ? (
        <Surface variant="secondary">
          <div className="p-4">
            <Typography color="muted" type="body-sm">
              Unlimited. Add a window to enforce a token budget.
            </Typography>
          </div>
        </Surface>
      ) : (
        limits.map((limit, index) => (
          <Surface key={index} variant="secondary">
            <div className="grid items-end gap-3 p-4 sm:grid-cols-[minmax(0,1.4fr)_minmax(0,0.7fr)_minmax(0,0.9fr)_auto]">
              <NumberField
                fullWidth
                isRequired
                maxValue={9_007_199_254_740_991}
                minValue={1}
                value={limit.tokenLimit}
                onChange={(value) => update(index, { tokenLimit: value })}
              >
                <Label>Token limit {index + 1}</Label>
                <NumberField.Group>
                  <NumberField.DecrementButton>
                    <Minus aria-hidden="true" size={15} />
                  </NumberField.DecrementButton>
                  <NumberField.Input />
                  <NumberField.IncrementButton>
                    <Plus aria-hidden="true" size={15} />
                  </NumberField.IncrementButton>
                </NumberField.Group>
              </NumberField>
              <NumberField
                fullWidth
                isRequired
                maxValue={10_000}
                minValue={1}
                value={limit.intervalCount}
                onChange={(value) => update(index, { intervalCount: value })}
              >
                <Label>Interval {index + 1}</Label>
                <NumberField.Group>
                  <NumberField.DecrementButton>
                    <Minus aria-hidden="true" size={15} />
                  </NumberField.DecrementButton>
                  <NumberField.Input />
                  <NumberField.IncrementButton>
                    <Plus aria-hidden="true" size={15} />
                  </NumberField.IncrementButton>
                </NumberField.Group>
              </NumberField>
              <Select
                fullWidth
                isRequired
                selectedKey={limit.intervalUnit}
                onSelectionChange={(key) => {
                  const unit = String(key);
                  if (isIntervalUnit(unit)) update(index, { intervalUnit: unit });
                }}
              >
                <Label>Unit {index + 1}</Label>
                <Select.Trigger>
                  <Select.Value />
                  <Select.Indicator />
                </Select.Trigger>
                <Select.Popover>
                  <ListBox>
                    {intervalUnits.map((unit) => (
                      <ListBox.Item id={unit} key={unit} textValue={pluralIntervalUnit(unit)}>
                        {pluralIntervalUnit(unit)}
                        <ListBox.ItemIndicator />
                      </ListBox.Item>
                    ))}
                  </ListBox>
                </Select.Popover>
              </Select>
              <Button
                aria-label={`Remove usage limit ${index + 1}`}
                size="sm"
                variant="danger-soft"
                onPress={() => onChange(limits.filter((_, position) => position !== index))}
              >
                <Trash2 aria-hidden="true" size={15} />
              </Button>
            </div>
          </Surface>
        ))
      )}
      {hasDuplicateUsageWindows(limits) ? (
        <Alert status="danger">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Title>Duplicate usage window</Alert.Title>
            <Alert.Description>Each interval window must be unique.</Alert.Description>
          </Alert.Content>
        </Alert>
      ) : (
        <Typography color="muted" type="body-xs">
          Up to eight limits. For example, combine an hourly guardrail with a weekly budget.
        </Typography>
      )}
    </div>
  );
}

function EmptyCard({ description, title }: { description: string; title: string }) {
  return (
    <Card>
      <Card.Content>
        <div className="flex min-h-52 flex-col items-center justify-center gap-3 text-center">
          <KeyRound aria-hidden="true" size={24} />
          <div>
            <Typography align="center" weight="medium">
              {title}
            </Typography>
            <Typography align="center" color="muted" type="body-sm">
              {description}
            </Typography>
          </div>
        </div>
      </Card.Content>
    </Card>
  );
}

function LoadingState({ label }: { label: string }) {
  return (
    <div className="flex min-h-52 items-center justify-center">
      <Spinner aria-label={label} />
    </div>
  );
}

function ErrorAlert({ message }: { message: string }) {
  return (
    <Alert status="danger">
      <Alert.Indicator>
        <AlertTriangle aria-hidden="true" size={18} />
      </Alert.Indicator>
      <Alert.Content>
        <Alert.Title>Gateway request failed</Alert.Title>
        <Alert.Description>{message}</Alert.Description>
      </Alert.Content>
    </Alert>
  );
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(new Date(value));
}

function formatDateTime(value: string): string {
  return new Intl.DateTimeFormat(undefined, {
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    month: 'short',
    timeZone: 'UTC',
    timeZoneName: 'short',
    year: 'numeric',
  }).format(new Date(value));
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value);
}

const intervalUnits: readonly IntervalUnit[] = ['minute', 'hour', 'day', 'week', 'month'];

function isIntervalUnit(value: string): value is IntervalUnit {
  return intervalUnits.some((unit) => unit === value);
}

function pluralIntervalUnit(unit: IntervalUnit): string {
  return `${unit}s`;
}

function formatInterval(limit: Pick<UsageLimit, 'intervalCount' | 'intervalUnit'>): string {
  return `${formatNumber(limit.intervalCount)} ${
    limit.intervalCount === 1 ? limit.intervalUnit : pluralIntervalUnit(limit.intervalUnit)
  }`;
}

function formatIntervalAdjective(
  limit: Pick<UsageLimit, 'intervalCount' | 'intervalUnit'>,
): string {
  return `${formatNumber(limit.intervalCount)}-${limit.intervalUnit}`;
}

function formatUsageLimit(limit: UsageLimit): string {
  return `${formatNumber(limit.tokenLimit)} every ${formatInterval(limit)}`;
}

function hasDuplicateUsageWindows(limits: UsageLimit[]): boolean {
  const windows = limits.map((limit) => `${limit.intervalCount}:${limit.intervalUnit}`);
  return new Set(windows).size !== windows.length;
}

function usageLimitsAreValid(limits: UsageLimit[]): boolean {
  return (
    limits.length <= 8 &&
    !hasDuplicateUsageWindows(limits) &&
    limits.every(
      (limit) =>
        Number.isSafeInteger(limit.tokenLimit) &&
        limit.tokenLimit > 0 &&
        Number.isSafeInteger(limit.intervalCount) &&
        limit.intervalCount > 0 &&
        limit.intervalCount <= 10_000,
    )
  );
}

function errorMessage(cause: unknown, fallback: string): string {
  return cause instanceof Error ? cause.message : fallback;
}
