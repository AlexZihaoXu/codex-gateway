import { isRecord, readJSONResponse } from './http';

export interface Project {
  createdAt: string;
  id: string;
  limits: UsageLimit[];
  name: string;
}

export type IntervalUnit = 'minute' | 'hour' | 'day' | 'week' | 'month';

export interface UsageLimit {
  intervalCount: number;
  intervalUnit: IntervalUnit;
  tokenLimit: number;
}

export interface UsageLimitUsage extends UsageLimit {
  cachedInputTokens: number;
  inputTokens: number;
  outputTokens: number;
  periodEnd: string;
  periodStart: string;
  reasoningOutputTokens: number;
  requests: number;
  totalTokens: number;
}

export interface APIKey {
  createdAt: string;
  id: string;
  lastUsedAt?: string;
  name: string;
  prefix: string;
  projectID: string;
  revokedAt?: string;
}

export interface CreatedAPIKey {
  apiKey: APIKey;
  secret: string;
}

export interface ProjectUsage {
  cachedInputTokens: number;
  inputTokens: number;
  limits: UsageLimitUsage[];
  name: string;
  outputTokens: number;
  projectID: string;
  reasoningOutputTokens: number;
  requests: number;
  totalTokens: number;
}

export interface APIKeyUsage {
  apiKeyID: string;
  cachedInputTokens: number;
  inputTokens: number;
  name: string;
  outputTokens: number;
  prefix: string;
  projectID: string;
  reasoningOutputTokens: number;
  requests: number;
  totalTokens: number;
}

export interface ModelUsage {
  cachedInputTokens: number;
  inputTokens: number;
  model: string;
  outputTokens: number;
  reasoningOutputTokens: number;
  requests: number;
  totalTokens: number;
}

export interface UsageSummary {
  apiKeys: APIKeyUsage[];
  cachedInputTokens: number;
  inputTokens: number;
  models: ModelUsage[];
  outputTokens: number;
  periodEnd: string;
  periodStart: string;
  projects: ProjectUsage[];
  reasoningOutputTokens: number;
  requests: number;
  totalTokens: number;
}

export async function listProjects(signal?: AbortSignal): Promise<Project[]> {
  const body = await getJSON('/api/projects', signal);
  if (!isRecord(body) || !Array.isArray(body.projects)) {
    throw new Error('The gateway returned an invalid project list.');
  }
  const projects = body.projects.map(parseProject);
  if (projects.some((project) => project === undefined)) {
    throw new Error('The gateway returned an invalid project list.');
  }
  return projects.filter((project): project is Project => project !== undefined);
}

export async function createProject(name: string, limits: UsageLimit[] = []): Promise<Project> {
  const request = limits.length === 0 ? { name } : { limits: limits.map(usageLimitRequest), name };
  const body = await postJSON('/api/projects', request);
  const project = parseProject(body);
  if (project === undefined) {
    throw new Error('The gateway returned an invalid project creation response.');
  }
  return project;
}

export async function updateProject(
  id: string,
  name: string,
  limits: UsageLimit[] = [],
): Promise<Project> {
  const request = limits.length === 0 ? { name } : { limits: limits.map(usageLimitRequest), name };
  const response = await fetch(`/api/projects/${encodeURIComponent(id)}`, {
    body: JSON.stringify(request),
    credentials: 'same-origin',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    method: 'PUT',
  });
  const project = parseProject(await readJSONResponse(response));
  if (project === undefined) {
    throw new Error('The gateway returned an invalid project update response.');
  }
  return project;
}

export async function deleteProject(id: string): Promise<void> {
  const response = await fetch(`/api/projects/${encodeURIComponent(id)}`, {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
    method: 'DELETE',
  });
  await readJSONResponse(response);
}

export async function listAPIKeys(signal?: AbortSignal): Promise<APIKey[]> {
  const body = await getJSON('/api/api-keys', signal);
  if (!isRecord(body) || !Array.isArray(body.api_keys)) {
    throw new Error('The gateway returned an invalid API key list.');
  }
  const keys = body.api_keys.map(parseAPIKey);
  if (keys.some((key) => key === undefined)) {
    throw new Error('The gateway returned an invalid API key list.');
  }
  return keys.filter((key): key is APIKey => key !== undefined);
}

export async function createAPIKey(projectID: string, name: string): Promise<CreatedAPIKey> {
  const body = await postJSON('/api/api-keys', { name, project_id: projectID });
  if (!isRecord(body) || typeof body.secret !== 'string') {
    throw new Error('The gateway returned an invalid API key creation response.');
  }
  const apiKey = parseAPIKey(body.api_key);
  if (apiKey === undefined) {
    throw new Error('The gateway returned an invalid API key creation response.');
  }
  return { apiKey, secret: body.secret };
}

export async function revokeAPIKey(id: string): Promise<void> {
  const response = await fetch(`/api/api-keys/${encodeURIComponent(id)}`, {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
    method: 'DELETE',
  });
  await readJSONResponse(response);
}

export async function readUsageSummary(signal?: AbortSignal): Promise<UsageSummary> {
  const body = await getJSON('/api/usage', signal);
  const usage = parseUsageSummary(body);
  if (usage === undefined) {
    throw new Error('The gateway returned an invalid usage summary.');
  }
  return usage;
}

async function getJSON(path: string, signal?: AbortSignal): Promise<unknown> {
  const response = await fetch(path, {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
    signal: signal ?? null,
  });
  return readJSONResponse(response);
}

async function postJSON(path: string, body: object): Promise<unknown> {
  const response = await fetch(path, {
    body: JSON.stringify(body),
    credentials: 'same-origin',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    method: 'POST',
  });
  return readJSONResponse(response);
}

function parseProject(value: unknown): Project | undefined {
  if (
    !isRecord(value) ||
    typeof value.id !== 'string' ||
    typeof value.name !== 'string' ||
    !isDateTime(value.created_at) ||
    !Array.isArray(value.limits) ||
    value.limits.length > 8
  ) {
    return undefined;
  }
  const limits = value.limits.map(parseUsageLimit);
  if (limits.some((limit) => limit === undefined)) {
    return undefined;
  }
  return {
    createdAt: value.created_at,
    id: value.id,
    limits: limits.filter((limit): limit is UsageLimit => limit !== undefined),
    name: value.name,
  };
}

function parseAPIKey(value: unknown): APIKey | undefined {
  if (
    !isRecord(value) ||
    typeof value.id !== 'string' ||
    typeof value.project_id !== 'string' ||
    typeof value.name !== 'string' ||
    typeof value.prefix !== 'string' ||
    !isDateTime(value.created_at) ||
    !isOptionalDateTime(value.last_used_at) ||
    !isOptionalDateTime(value.revoked_at)
  ) {
    return undefined;
  }
  const key: APIKey = {
    createdAt: value.created_at,
    id: value.id,
    name: value.name,
    prefix: value.prefix,
    projectID: value.project_id,
  };
  if (typeof value.last_used_at === 'string') {
    key.lastUsedAt = value.last_used_at;
  }
  if (typeof value.revoked_at === 'string') {
    key.revokedAt = value.revoked_at;
  }
  return key;
}

function parseUsageSummary(value: unknown): UsageSummary | undefined {
  if (
    !isRecord(value) ||
    !isDateTime(value.period_start) ||
    !isDateTime(value.period_end) ||
    !Array.isArray(value.projects) ||
    !Array.isArray(value.api_keys) ||
    !Array.isArray(value.models) ||
    !isNonnegativeInteger(value.requests) ||
    !isNonnegativeInteger(value.input_tokens) ||
    !isNonnegativeInteger(value.cached_input_tokens) ||
    !isNonnegativeInteger(value.output_tokens) ||
    !isNonnegativeInteger(value.reasoning_output_tokens) ||
    !isNonnegativeInteger(value.total_tokens)
  ) {
    return undefined;
  }
  const projects = value.projects.map(parseProjectUsage);
  if (projects.some((project) => project === undefined)) {
    return undefined;
  }
  const apiKeys = value.api_keys.map(parseAPIKeyUsage);
  if (apiKeys.some((key) => key === undefined)) {
    return undefined;
  }
  const models = value.models.map(parseModelUsage);
  if (models.some((model) => model === undefined)) {
    return undefined;
  }
  return {
    apiKeys: apiKeys.filter((key): key is APIKeyUsage => key !== undefined),
    cachedInputTokens: value.cached_input_tokens,
    inputTokens: value.input_tokens,
    models: models.filter((model): model is ModelUsage => model !== undefined),
    outputTokens: value.output_tokens,
    periodEnd: value.period_end,
    periodStart: value.period_start,
    projects: projects.filter((project): project is ProjectUsage => project !== undefined),
    reasoningOutputTokens: value.reasoning_output_tokens,
    requests: value.requests,
    totalTokens: value.total_tokens,
  };
}

function parseModelUsage(value: unknown): ModelUsage | undefined {
  if (
    !isRecord(value) ||
    typeof value.model !== 'string' ||
    value.model.length === 0 ||
    value.model.length > 128 ||
    !isNonnegativeInteger(value.requests) ||
    !isNonnegativeInteger(value.input_tokens) ||
    !isNonnegativeInteger(value.cached_input_tokens) ||
    !isNonnegativeInteger(value.output_tokens) ||
    !isNonnegativeInteger(value.reasoning_output_tokens) ||
    !isNonnegativeInteger(value.total_tokens) ||
    value.cached_input_tokens > value.input_tokens ||
    value.reasoning_output_tokens > value.output_tokens ||
    value.total_tokens !== value.input_tokens + value.output_tokens
  ) {
    return undefined;
  }
  return {
    cachedInputTokens: value.cached_input_tokens,
    inputTokens: value.input_tokens,
    model: value.model,
    outputTokens: value.output_tokens,
    reasoningOutputTokens: value.reasoning_output_tokens,
    requests: value.requests,
    totalTokens: value.total_tokens,
  };
}

function parseProjectUsage(value: unknown): ProjectUsage | undefined {
  if (
    !isRecord(value) ||
    typeof value.project_id !== 'string' ||
    typeof value.name !== 'string' ||
    !Array.isArray(value.limits) ||
    value.limits.length > 8 ||
    !isNonnegativeInteger(value.requests) ||
    !isNonnegativeInteger(value.input_tokens) ||
    !isNonnegativeInteger(value.cached_input_tokens) ||
    !isNonnegativeInteger(value.output_tokens) ||
    !isNonnegativeInteger(value.reasoning_output_tokens) ||
    !isNonnegativeInteger(value.total_tokens)
  ) {
    return undefined;
  }
  const limits = value.limits.map(parseUsageLimitUsage);
  if (limits.some((limit) => limit === undefined)) {
    return undefined;
  }
  const project: ProjectUsage = {
    cachedInputTokens: value.cached_input_tokens,
    inputTokens: value.input_tokens,
    limits: limits.filter((limit): limit is UsageLimitUsage => limit !== undefined),
    name: value.name,
    outputTokens: value.output_tokens,
    projectID: value.project_id,
    reasoningOutputTokens: value.reasoning_output_tokens,
    requests: value.requests,
    totalTokens: value.total_tokens,
  };
  return project;
}

function parseUsageLimit(value: unknown): UsageLimit | undefined {
  if (
    !isRecord(value) ||
    !isPositiveSafeInteger(value.token_limit) ||
    !isPositiveSafeInteger(value.interval_count) ||
    value.interval_count > 10_000 ||
    !isIntervalUnit(value.interval_unit)
  ) {
    return undefined;
  }
  return {
    intervalCount: value.interval_count,
    intervalUnit: value.interval_unit,
    tokenLimit: value.token_limit,
  };
}

function parseUsageLimitUsage(value: unknown): UsageLimitUsage | undefined {
  const limit = parseUsageLimit(value);
  if (
    limit === undefined ||
    !isRecord(value) ||
    !isDateTime(value.period_start) ||
    !isDateTime(value.period_end) ||
    !isNonnegativeInteger(value.requests) ||
    !isNonnegativeInteger(value.input_tokens) ||
    !isNonnegativeInteger(value.cached_input_tokens) ||
    !isNonnegativeInteger(value.output_tokens) ||
    !isNonnegativeInteger(value.reasoning_output_tokens) ||
    !isNonnegativeInteger(value.total_tokens)
  ) {
    return undefined;
  }
  return {
    ...limit,
    cachedInputTokens: value.cached_input_tokens,
    inputTokens: value.input_tokens,
    outputTokens: value.output_tokens,
    periodEnd: value.period_end,
    periodStart: value.period_start,
    reasoningOutputTokens: value.reasoning_output_tokens,
    requests: value.requests,
    totalTokens: value.total_tokens,
  };
}

function parseAPIKeyUsage(value: unknown): APIKeyUsage | undefined {
  if (
    !isRecord(value) ||
    typeof value.api_key_id !== 'string' ||
    typeof value.project_id !== 'string' ||
    typeof value.name !== 'string' ||
    typeof value.prefix !== 'string' ||
    !isNonnegativeInteger(value.requests) ||
    !isNonnegativeInteger(value.input_tokens) ||
    !isNonnegativeInteger(value.cached_input_tokens) ||
    !isNonnegativeInteger(value.output_tokens) ||
    !isNonnegativeInteger(value.reasoning_output_tokens) ||
    !isNonnegativeInteger(value.total_tokens)
  ) {
    return undefined;
  }
  return {
    apiKeyID: value.api_key_id,
    cachedInputTokens: value.cached_input_tokens,
    inputTokens: value.input_tokens,
    name: value.name,
    outputTokens: value.output_tokens,
    prefix: value.prefix,
    projectID: value.project_id,
    reasoningOutputTokens: value.reasoning_output_tokens,
    requests: value.requests,
    totalTokens: value.total_tokens,
  };
}

function isNonnegativeInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
}

function isPositiveSafeInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0;
}

function isIntervalUnit(value: unknown): value is IntervalUnit {
  return ['minute', 'hour', 'day', 'week', 'month'].includes(String(value));
}

function usageLimitRequest(limit: UsageLimit) {
  return {
    interval_count: limit.intervalCount,
    interval_unit: limit.intervalUnit,
    token_limit: limit.tokenLimit,
  };
}

function isDateTime(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && !Number.isNaN(Date.parse(value));
}

function isOptionalDateTime(value: unknown): boolean {
  return value === undefined || isDateTime(value);
}
