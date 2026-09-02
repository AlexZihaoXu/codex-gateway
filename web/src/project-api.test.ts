import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  createAPIKey,
  createProject,
  deleteProject,
  listAPIKeys,
  listProjects,
  readUsageSummary,
  revokeAPIKey,
  updateProject,
} from './project-api';

describe('project management API client', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('lists and creates projects with configurable usage limits', async () => {
    const fetchMock = mockFetch(
      jsonResponse({
        projects: [
          {
            created_at: '2026-09-02T15:00:00Z',
            id: 'prj_1',
            limits: [{ interval_count: 6, interval_unit: 'hour', token_limit: 250000 }],
            name: 'OpenCode',
          },
          { created_at: '2026-09-02T16:00:00Z', id: 'prj_2', limits: [], name: 'Local scripts' },
        ],
      }),
      jsonResponse(
        {
          created_at: '2026-09-02T15:00:00Z',
          id: 'prj_1',
          limits: [{ interval_count: 6, interval_unit: 'hour', token_limit: 250000 }],
          name: 'OpenCode',
        },
        201,
      ),
    );

    await expect(listProjects()).resolves.toEqual([
      {
        createdAt: '2026-09-02T15:00:00Z',
        id: 'prj_1',
        limits: [{ intervalCount: 6, intervalUnit: 'hour', tokenLimit: 250000 }],
        name: 'OpenCode',
      },
      { createdAt: '2026-09-02T16:00:00Z', id: 'prj_2', limits: [], name: 'Local scripts' },
    ]);
    await createProject('OpenCode', [
      { intervalCount: 6, intervalUnit: 'hour', tokenLimit: 250000 },
    ]);

    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/projects',
      expect.objectContaining({
        body: JSON.stringify({
          limits: [{ interval_count: 6, interval_unit: 'hour', token_limit: 250000 }],
          name: 'OpenCode',
        }),
        method: 'POST',
      }),
    );
  });

  it('omits an empty project limit list from creation', async () => {
    const fetchMock = mockFetch(
      jsonResponse(
        { created_at: '2026-09-02T15:00:00Z', id: 'prj_1', limits: [], name: 'OpenCode' },
        201,
      ),
    );

    await createProject('OpenCode');

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/projects',
      expect.objectContaining({ body: JSON.stringify({ name: 'OpenCode' }) }),
    );
  });

  it('updates and deletes projects using encoded identifiers', async () => {
    const fetchMock = mockFetch(
      jsonResponse({
        created_at: '2026-09-02T15:00:00Z',
        id: 'prj/1',
        limits: [{ interval_count: 1, interval_unit: 'day', token_limit: 500000 }],
        name: 'OpenCode Desktop',
      }),
      jsonResponse({ status: 'ok' }),
    );

    await expect(
      updateProject('prj/1', 'OpenCode Desktop', [
        { intervalCount: 1, intervalUnit: 'day', tokenLimit: 500000 },
      ]),
    ).resolves.toMatchObject({
      id: 'prj/1',
      limits: [{ intervalCount: 1, intervalUnit: 'day', tokenLimit: 500000 }],
      name: 'OpenCode Desktop',
    });
    await deleteProject('prj/1');

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/api/projects/prj%2F1',
      expect.objectContaining({
        body: JSON.stringify({
          limits: [{ interval_count: 1, interval_unit: 'day', token_limit: 500000 }],
          name: 'OpenCode Desktop',
        }),
        method: 'PUT',
      }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/projects/prj%2F1',
      expect.objectContaining({ method: 'DELETE' }),
    );
  });

  it('lists, creates, and revokes API keys without expecting listed secrets', async () => {
    const key = {
      created_at: '2026-09-02T15:00:00Z',
      id: 'key_1',
      name: 'Development',
      prefix: 'cg_example',
      project_id: 'prj_1',
    };
    const fetchMock = mockFetch(
      jsonResponse({ api_keys: [key] }),
      jsonResponse({ api_key: key, secret: 'cg_plaintext_once' }, 201),
      jsonResponse({ status: 'ok' }),
    );

    await expect(listAPIKeys()).resolves.toEqual([
      {
        createdAt: '2026-09-02T15:00:00Z',
        id: 'key_1',
        name: 'Development',
        prefix: 'cg_example',
        projectID: 'prj_1',
      },
    ]);
    await expect(createAPIKey('prj_1', 'Development')).resolves.toMatchObject({
      apiKey: { id: 'key_1' },
      secret: 'cg_plaintext_once',
    });
    await revokeAPIKey('key/1');

    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/api-keys/key%2F1',
      expect.objectContaining({ method: 'DELETE' }),
    );
  });

  it('maps current-month totals and active project limit windows', async () => {
    mockFetch(
      jsonResponse({
        api_keys: [
          {
            api_key_id: 'key_1',
            cached_input_tokens: 35,
            input_tokens: 200,
            name: 'Development',
            output_tokens: 50,
            prefix: 'cg_example',
            project_id: 'prj_1',
            reasoning_output_tokens: 13,
            requests: 2,
            total_tokens: 250,
          },
        ],
        cached_input_tokens: 35,
        input_tokens: 200,
        output_tokens: 50,
        period_end: '2026-10-01T00:00:00Z',
        period_start: '2026-09-01T00:00:00Z',
        projects: [
          {
            input_tokens: 200,
            cached_input_tokens: 35,
            limits: [
              {
                cached_input_tokens: 35,
                input_tokens: 200,
                interval_count: 6,
                interval_unit: 'hour',
                output_tokens: 50,
                period_end: '2026-09-02T18:00:00Z',
                period_start: '2026-09-02T12:00:00Z',
                reasoning_output_tokens: 13,
                requests: 2,
                token_limit: 1000,
                total_tokens: 250,
              },
            ],
            name: 'OpenCode',
            output_tokens: 50,
            project_id: 'prj_1',
            requests: 2,
            reasoning_output_tokens: 13,
            total_tokens: 250,
          },
        ],
        requests: 2,
        reasoning_output_tokens: 13,
        total_tokens: 250,
      }),
    );

    await expect(readUsageSummary()).resolves.toMatchObject({
      apiKeys: [
        {
          apiKeyID: 'key_1',
          cachedInputTokens: 35,
          prefix: 'cg_example',
          projectID: 'prj_1',
          reasoningOutputTokens: 13,
          totalTokens: 250,
        },
      ],
      cachedInputTokens: 35,
      inputTokens: 200,
      outputTokens: 50,
      projects: [
        {
          cachedInputTokens: 35,
          limits: [
            {
              intervalCount: 6,
              intervalUnit: 'hour',
              periodEnd: '2026-09-02T18:00:00Z',
              periodStart: '2026-09-02T12:00:00Z',
              tokenLimit: 1000,
              totalTokens: 250,
            },
          ],
          projectID: 'prj_1',
          reasoningOutputTokens: 13,
          totalTokens: 250,
        },
      ],
      reasoningOutputTokens: 13,
      requests: 2,
      totalTokens: 250,
    });
  });

  it('rejects malformed project, API key, and usage responses', async () => {
    mockFetch(
      jsonResponse({ projects: [{ id: 'prj_1' }] }),
      jsonResponse({ api_keys: [{ id: 'key_1' }] }),
      jsonResponse({ api_key: {}, secret: 123 }),
      jsonResponse({ projects: [], requests: -1 }),
      jsonResponse({
        projects: [
          {
            created_at: '2026-09-02T15:00:00Z',
            id: 'prj_1',
            limits: [{ interval_count: 10001, interval_unit: 'hour', token_limit: 1000 }],
            name: 'Unsafe interval',
          },
        ],
      }),
    );

    await expect(listProjects()).rejects.toThrow('invalid project list');
    await expect(listAPIKeys()).rejects.toThrow('invalid API key list');
    await expect(createAPIKey('prj_1', 'Development')).rejects.toThrow(
      'invalid API key creation response',
    );
    await expect(readUsageSummary()).rejects.toThrow('invalid usage summary');
    await expect(listProjects()).rejects.toThrow('invalid project list');
  });
});

function jsonResponse(body: object, status = 200): Response {
  return new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
    status,
  });
}

function mockFetch(...responses: Response[]) {
  const fetchMock = vi.fn();
  for (const response of responses) {
    fetchMock.mockResolvedValueOnce(response);
  }
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}
