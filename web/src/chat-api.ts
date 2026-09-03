import { isRecord, readJSONResponse } from './http';

export type PlaygroundEndpoint = 'chat-completions' | 'responses';
export type ReasoningEffort = 'none' | 'low' | 'medium' | 'high' | 'xhigh' | 'max' | 'ultra';
export type PlaygroundRole = 'assistant' | 'developer' | 'system' | 'user';

export interface PlaygroundMessage {
  content: string;
  role: PlaygroundRole;
}

export interface PlaygroundUsage {
  cachedInputTokens: number;
  inputTokens: number;
  outputTokens: number;
  reasoningOutputTokens: number;
  totalTokens: number;
}

export interface PlaygroundStreamResult {
  reasoning?: string;
  text: string;
  usage: PlaygroundUsage;
  webSearches?: WebSearchActivity[];
}

export interface WebSearchResult {
  refID?: string;
  snippet?: string;
  title?: string;
  type?: string;
  url?: string;
}

export interface WebSearchActivity {
  callID: string;
  query: string;
  results: WebSearchResult[];
  status: 'completed' | 'running';
}

export interface PersistedChatMessage {
  content: string;
  reasoning?: string;
  role: 'assistant' | 'user';
  usage?: PlaygroundUsage;
  webSearches?: WebSearchActivity[];
}

export interface PersistedChatSession {
  createdAt: string;
  endpoint: PlaygroundEndpoint;
  id: string;
  instructions: string;
  messages: PersistedChatMessage[];
  model: string;
  projectID: string;
  reasoningEffort: ReasoningEffort;
  webSearch: boolean;
  restartSession?: boolean;
  title: string;
  updatedAt: string;
}

export interface PlaygroundRequest {
  endpoint: PlaygroundEndpoint;
  messages: PlaygroundMessage[];
  model: string;
  projectID?: string;
  reasoningEffort: ReasoningEffort;
  restartSession?: boolean;
  sessionID: string;
  signal?: AbortSignal;
  title?: string;
  webSearch?: boolean;
}

export async function listPlaygroundModels(signal?: AbortSignal): Promise<string[]> {
  const response = await fetch('/api/chat/models', {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
    signal: signal ?? null,
  });
  const body = await readJSONResponse(response);
  if (!isRecord(body) || body.object !== 'list' || !Array.isArray(body.data)) {
    throw new Error('The gateway returned an invalid model list.');
  }
  const models = body.data.map((item) => (isRecord(item) ? item.id : undefined));
  if (models.some((model) => typeof model !== 'string')) {
    throw new Error('The gateway returned an invalid model list.');
  }
  return models.filter((model): model is string => typeof model === 'string');
}

export async function listChatSessions(signal?: AbortSignal): Promise<PersistedChatSession[]> {
  const response = await fetch('/api/chat/sessions', {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
    signal: signal ?? null,
  });
  const body = await readJSONResponse(response);
  if (!isRecord(body) || !Array.isArray(body.sessions)) {
    throw new Error('The gateway returned invalid chat history.');
  }
  const sessions = body.sessions.map(parsePersistedChatSession);
  if (sessions.some((session) => session === undefined)) {
    throw new Error('The gateway returned invalid chat history.');
  }
  return sessions.filter((session): session is PersistedChatSession => session !== undefined);
}

export async function renameChatSession(sessionID: string, title: string): Promise<void> {
  const response = await fetch(`/api/chat/sessions/${encodeURIComponent(sessionID)}`, {
    body: JSON.stringify({ title }),
    credentials: 'same-origin',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    method: 'PATCH',
  });
  if (!response.ok) await readJSONResponse(response);
}

export async function deleteChatSession(sessionID: string): Promise<void> {
  const response = await fetch(`/api/chat/sessions/${encodeURIComponent(sessionID)}`, {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
    method: 'DELETE',
  });
  if (!response.ok) await readJSONResponse(response);
}

export async function streamPlayground(
  request: PlaygroundRequest,
  onDelta: (delta: string) => void,
  onReasoningDelta: (delta: string) => void = () => undefined,
  onWebSearch: (activity: WebSearchActivity) => void = () => undefined,
): Promise<PlaygroundStreamResult> {
  const response = await fetch('/api/chat/stream', {
    body: JSON.stringify({
      endpoint: request.endpoint,
      messages: request.messages,
      model: request.model,
      ...(request.projectID ? { project_id: request.projectID } : {}),
      reasoning_effort: request.reasoningEffort,
      ...(request.restartSession ? { restart_session: true } : {}),
      session_id: request.sessionID,
      ...(request.title ? { title: request.title } : {}),
      ...(request.webSearch ? { web_search: true } : {}),
    }),
    credentials: 'same-origin',
    headers: {
      Accept: 'text/event-stream',
      'Content-Type': 'application/json',
    },
    method: 'POST',
    signal: request.signal ?? null,
  });
  if (!response.ok) {
    await readJSONResponse(response);
    throw new Error(`The gateway request failed with status ${response.status}.`);
  }
  if (!response.body) {
    throw new Error('The gateway returned an empty response stream.');
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  let text = '';
  let reasoning = '';
  let usage: PlaygroundUsage | undefined;
  const webSearches = new Map<string, WebSearchActivity>();

  function consume(block: string): boolean {
    const event = parseSSEBlock(block);
    if (!event) return false;
    if (event.data === '[DONE]') return true;
    let data: unknown;
    try {
      data = JSON.parse(event.data);
    } catch {
      throw new Error('The gateway returned a malformed response stream.');
    }
    const streamError = parseStreamError(data);
    if (streamError) throw new Error(streamError);
    const eventType =
      event.name || (isRecord(data) && typeof data.type === 'string' ? data.type : '');
    if (
      eventType === 'gateway.web_search.started' ||
      eventType === 'gateway.web_search.completed'
    ) {
      const activity = parseWebSearchActivity(data, eventType === 'gateway.web_search.completed');
      if (!activity) throw new Error('The gateway returned malformed web search activity.');
      webSearches.set(activity.callID, activity);
      onWebSearch(activity);
      return false;
    }

    if (request.endpoint === 'chat-completions') {
      const reasoningDelta = parseChatReasoningDelta(data);
      if (reasoningDelta) {
        reasoning += reasoningDelta;
        onReasoningDelta(reasoningDelta);
      }
      const delta = parseChatDelta(data);
      if (delta) {
        text += delta;
        onDelta(delta);
      }
      const terminalUsage = parseChatUsage(data);
      if (terminalUsage) usage = terminalUsage;
      return false;
    }

    if (eventType === 'response.failed') {
      throw new Error('Codex could not complete the response.');
    }
    if (
      (eventType === 'response.reasoning_text.delta' ||
        eventType === 'response.reasoning_summary.delta' ||
        eventType === 'response.reasoning_summary_text.delta') &&
      isRecord(data) &&
      typeof data.delta === 'string'
    ) {
      reasoning += data.delta;
      onReasoningDelta(data.delta);
    }
    if (
      eventType === 'response.output_text.delta' &&
      isRecord(data) &&
      typeof data.delta === 'string'
    ) {
      text += data.delta;
      onDelta(data.delta);
    }
    if (eventType === 'response.completed' && isRecord(data) && isRecord(data.response)) {
      const terminalUsage = parseResponseUsage(data.response.usage);
      if (terminalUsage) usage = terminalUsage;
    }
    return false;
  }

  let terminalSentinel = false;
  try {
    stream: while (true) {
      const chunk = await reader.read();
      if (chunk.done) break;
      buffer = (buffer + decoder.decode(chunk.value, { stream: true })).replaceAll('\r\n', '\n');
      let boundary = buffer.indexOf('\n\n');
      while (boundary >= 0) {
        if (consume(buffer.slice(0, boundary))) {
          terminalSentinel = true;
          break stream;
        }
        buffer = buffer.slice(boundary + 2);
        boundary = buffer.indexOf('\n\n');
      }
    }
    if (!terminalSentinel) {
      buffer = (buffer + decoder.decode()).replaceAll('\r\n', '\n');
      if (buffer.trim()) consume(buffer);
    }
  } finally {
    if (terminalSentinel) {
      try {
        await reader.cancel();
      } catch {
        // The terminal event is authoritative even if the transport already closed.
      }
    }
    reader.releaseLock();
  }
  if (!usage) {
    throw new Error('The gateway response ended without terminal usage data.');
  }
  const searches = [...webSearches.values()];
  return {
    ...(reasoning ? { reasoning } : {}),
    text,
    usage,
    ...(searches.length > 0 ? { webSearches: searches } : {}),
  };
}

function parseWebSearchActivity(value: unknown, completed: boolean): WebSearchActivity | undefined {
  if (
    !isRecord(value) ||
    !isBoundedString(value.call_id, 256, false) ||
    !isBoundedString(value.query, 4096, false)
  )
    return undefined;
  const results = value.results === undefined ? [] : parseWebSearchResults(value.results);
  if (!results) return undefined;
  return {
    callID: value.call_id,
    query: value.query,
    results,
    status: completed ? 'completed' : 'running',
  };
}

function parseWebSearchResults(value: unknown): WebSearchResult[] | undefined {
  if (!Array.isArray(value) || value.length > 100) return undefined;
  const results: WebSearchResult[] = [];
  for (const item of value) {
    if (!isRecord(item)) return undefined;
    const result: WebSearchResult = {};
    for (const [key, maximum] of [
      ['type', 128],
      ['ref_id', 256],
      ['url', 8192],
      ['title', 4096],
      ['snippet', 16384],
    ] as const) {
      const candidate = item[key];
      if (candidate !== undefined && !isBoundedString(candidate, maximum, true)) return undefined;
      if (typeof candidate !== 'string') continue;
      if (key === 'ref_id') result.refID = candidate;
      else result[key] = candidate;
    }
    results.push(result);
  }
  return results;
}

function parseSSEBlock(block: string): { data: string; name: string } | undefined {
  let name = '';
  const data: string[] = [];
  for (const line of block.split('\n')) {
    if (line.startsWith('event:')) name = line.slice(6).trim();
    if (line.startsWith('data:')) data.push(line.slice(5).trimStart());
  }
  return data.length > 0 ? { data: data.join('\n'), name } : undefined;
}

function parseStreamError(value: unknown): string | undefined {
  if (!isRecord(value) || !isRecord(value.error) || typeof value.error.message !== 'string') {
    return undefined;
  }
  return value.error.message;
}

function parseChatDelta(value: unknown): string | undefined {
  if (!isRecord(value) || !Array.isArray(value.choices)) return undefined;
  const choice = value.choices[0];
  if (!isRecord(choice) || !isRecord(choice.delta) || typeof choice.delta.content !== 'string') {
    return undefined;
  }
  return choice.delta.content;
}

function parseChatReasoningDelta(value: unknown): string | undefined {
  if (!isRecord(value) || !Array.isArray(value.choices)) return undefined;
  const choice = value.choices[0];
  if (
    !isRecord(choice) ||
    !isRecord(choice.delta) ||
    typeof choice.delta.reasoning_content !== 'string'
  ) {
    return undefined;
  }
  return choice.delta.reasoning_content;
}

function parseChatUsage(value: unknown): PlaygroundUsage | undefined {
  if (!isRecord(value) || !isRecord(value.usage)) return undefined;
  const usage = value.usage;
  if (
    !isTokenCount(usage.prompt_tokens) ||
    !isTokenCount(usage.completion_tokens) ||
    !isTokenCount(usage.total_tokens) ||
    !isRecord(usage.prompt_tokens_details) ||
    !isTokenCount(usage.prompt_tokens_details.cached_tokens) ||
    !isRecord(usage.completion_tokens_details) ||
    !isTokenCount(usage.completion_tokens_details.reasoning_tokens)
  ) {
    return undefined;
  }
  return {
    cachedInputTokens: usage.prompt_tokens_details.cached_tokens,
    inputTokens: usage.prompt_tokens,
    outputTokens: usage.completion_tokens,
    reasoningOutputTokens: usage.completion_tokens_details.reasoning_tokens,
    totalTokens: usage.total_tokens,
  };
}

function parseResponseUsage(value: unknown): PlaygroundUsage | undefined {
  if (
    !isRecord(value) ||
    !isTokenCount(value.input_tokens) ||
    !isTokenCount(value.output_tokens) ||
    !isTokenCount(value.total_tokens) ||
    !isRecord(value.input_tokens_details) ||
    !isTokenCount(value.input_tokens_details.cached_tokens) ||
    !isRecord(value.output_tokens_details) ||
    !isTokenCount(value.output_tokens_details.reasoning_tokens)
  ) {
    return undefined;
  }
  return {
    cachedInputTokens: value.input_tokens_details.cached_tokens,
    inputTokens: value.input_tokens,
    outputTokens: value.output_tokens,
    reasoningOutputTokens: value.output_tokens_details.reasoning_tokens,
    totalTokens: value.total_tokens,
  };
}

function isTokenCount(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
}

function parsePersistedChatSession(value: unknown): PersistedChatSession | undefined {
  if (
    !isRecord(value) ||
    !isBoundedString(value.id, 128, false) ||
    !isBoundedString(value.title, 200, false) ||
    !isBoundedString(value.model, 128, false) ||
    !isBoundedString(value.project_id, 128, true) ||
    !isPlaygroundEndpoint(value.endpoint) ||
    !isReasoningEffort(value.reasoning_effort) ||
    !isBoundedString(value.instructions, 4 * 1024 * 1024, true) ||
    !Array.isArray(value.messages) ||
    value.messages.length === 0 ||
    value.messages.length > 1025 ||
    !isDateTime(value.created_at) ||
    !isDateTime(value.updated_at)
  ) {
    return undefined;
  }
  const messages = value.messages.map(parsePersistedChatMessage);
  if (messages.some((message) => message === undefined)) return undefined;
  return {
    createdAt: value.created_at,
    endpoint: value.endpoint,
    id: value.id,
    instructions: value.instructions,
    messages: messages.filter((message): message is PersistedChatMessage => message !== undefined),
    model: value.model,
    projectID: value.project_id,
    reasoningEffort: value.reasoning_effort,
    webSearch: value.web_search === true,
    title: value.title,
    updatedAt: value.updated_at,
  };
}

function parsePersistedChatMessage(value: unknown): PersistedChatMessage | undefined {
  if (
    !isRecord(value) ||
    (value.role !== 'user' && value.role !== 'assistant') ||
    !isBoundedString(value.content, 1024 * 1024, true) ||
    (value.reasoning !== undefined && !isBoundedString(value.reasoning, 1024 * 1024, true))
  ) {
    return undefined;
  }
  const role: 'assistant' | 'user' = value.role;
  const base: PersistedChatMessage = {
    content: value.content,
    ...(typeof value.reasoning === 'string' && value.reasoning
      ? { reasoning: value.reasoning }
      : {}),
    role,
  };
  if (value.web_searches !== undefined) {
    const searches = parsePersistedWebSearches(value.web_searches);
    if (!searches) return undefined;
    base.webSearches = searches;
  }
  if (value.usage === undefined) return base;
  const usage = parsePersistedUsage(value.usage);
  if (!usage) return undefined;
  return { ...base, usage };
}

function parsePersistedWebSearches(value: unknown): WebSearchActivity[] | undefined {
  if (!Array.isArray(value) || value.length > 64) return undefined;
  const searches: WebSearchActivity[] = [];
  for (const item of value) {
    const activity = parseWebSearchActivity(item, true);
    if (!activity) return undefined;
    searches.push(activity);
  }
  return searches;
}

function parsePersistedUsage(value: unknown): PlaygroundUsage | undefined {
  if (
    !isRecord(value) ||
    !isTokenCount(value.input_tokens) ||
    !isTokenCount(value.cached_input_tokens) ||
    !isTokenCount(value.output_tokens) ||
    !isTokenCount(value.reasoning_output_tokens) ||
    !isTokenCount(value.total_tokens) ||
    value.cached_input_tokens > value.input_tokens ||
    value.reasoning_output_tokens > value.output_tokens ||
    value.total_tokens !== value.input_tokens + value.output_tokens
  ) {
    return undefined;
  }
  return {
    cachedInputTokens: value.cached_input_tokens,
    inputTokens: value.input_tokens,
    outputTokens: value.output_tokens,
    reasoningOutputTokens: value.reasoning_output_tokens,
    totalTokens: value.total_tokens,
  };
}

function isPlaygroundEndpoint(value: unknown): value is PlaygroundEndpoint {
  return value === 'chat-completions' || value === 'responses';
}

function isReasoningEffort(value: unknown): value is ReasoningEffort {
  return (
    value === 'none' ||
    value === 'low' ||
    value === 'medium' ||
    value === 'high' ||
    value === 'xhigh' ||
    value === 'max' ||
    value === 'ultra'
  );
}

function isBoundedString(value: unknown, maximum: number, allowEmpty: boolean): value is string {
  return typeof value === 'string' && value.length <= maximum && (allowEmpty || value.length > 0);
}

function isDateTime(value: unknown): value is string {
  return typeof value === 'string' && value.length <= 64 && Number.isFinite(Date.parse(value));
}
