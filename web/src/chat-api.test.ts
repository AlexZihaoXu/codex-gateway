import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  deleteChatSession,
  listChatSessions,
  listPlaygroundModels,
  renameChatSession,
  streamPlayground,
} from './chat-api';

describe('chat playground API', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('lists models with the administrator session and no project API key', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        data: [
          { created: 0, id: 'gpt-5.6-sol', object: 'model', owned_by: 'codex-gateway' },
          { created: 0, id: 'gpt-5.6-terra', object: 'model', owned_by: 'codex-gateway' },
        ],
        object: 'list',
      }),
    );
    vi.stubGlobal('fetch', fetchMock);

    await expect(listPlaygroundModels()).resolves.toEqual(['gpt-5.6-sol', 'gpt-5.6-terra']);
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/chat/models',
      expect.objectContaining({
        credentials: 'same-origin',
        headers: { Accept: 'application/json' },
      }),
    );
  });

  it('loads owner-scoped persisted chat sessions and terminal usage', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        sessions: [
          {
            created_at: '2026-09-03T12:00:00Z',
            endpoint: 'chat-completions',
            id: 'chat-session-1',
            instructions: 'Be concise.',
            messages: [
              { content: 'Say hello', role: 'user' },
              {
                content: 'Hello',
                reasoning: 'Checked the request.',
                role: 'assistant',
                usage: {
                  cached_input_tokens: 4,
                  input_tokens: 12,
                  output_tokens: 7,
                  reasoning_output_tokens: 3,
                  total_tokens: 19,
                },
              },
            ],
            model: 'gpt-5.6-sol',
            project_id: 'prj_editor',
            reasoning_effort: 'medium',
            title: 'Say hello',
            updated_at: '2026-09-03T12:01:00Z',
          },
        ],
      }),
    );
    vi.stubGlobal('fetch', fetchMock);

    await expect(listChatSessions()).resolves.toEqual([
      expect.objectContaining({
        id: 'chat-session-1',
        projectID: 'prj_editor',
        messages: [
          { content: 'Say hello', role: 'user' },
          {
            content: 'Hello',
            reasoning: 'Checked the request.',
            role: 'assistant',
            usage: {
              cachedInputTokens: 4,
              inputTokens: 12,
              outputTokens: 7,
              reasoningOutputTokens: 3,
              totalTokens: 19,
            },
          },
        ],
      }),
    ]);
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/chat/sessions',
      expect.objectContaining({ credentials: 'same-origin' }),
    );
  });

  it('renames and deletes persisted chat sessions with the administrator session', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(renameChatSession('chat-session-1', 'Renamed chat')).resolves.toBeUndefined();
    await expect(deleteChatSession('chat-session-1')).resolves.toBeUndefined();

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/api/chat/sessions/chat-session-1',
      expect.objectContaining({
        body: JSON.stringify({ title: 'Renamed chat' }),
        credentials: 'same-origin',
        method: 'PATCH',
      }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/chat/sessions/chat-session-1',
      expect.objectContaining({ credentials: 'same-origin', method: 'DELETE' }),
    );
  });

  it('rejects malformed persisted chat history at the API boundary', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        jsonResponse({
          sessions: [{ id: 'chat-session-1', messages: [{ content: 'Hi', role: 'system' }] }],
        }),
      ),
    );

    await expect(listChatSessions()).rejects.toThrow('invalid chat history');
  });

  it('streams chat completion deltas and captures terminal usage', async () => {
    const onDelta = vi.fn();
    const onReasoningDelta = vi.fn();
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        sseResponse([
          'data: {"choices":[{"delta":{"role":"assistant","content":""}}]}\n\n',
          'data: {"choices":[{"delta":{"reasoning_content":"Checked"}}]}\n\n',
          'data: {"choices":[{"delta":{"content":"Hel',
          'lo"}}]}\n\ndata: {"choices":[],"usage":{"prompt_tokens":12,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens":7,"completion_tokens_details":{"reasoning_tokens":3},"total_tokens":19}}\n\ndata: [DONE]\n\n',
        ]),
      );
    vi.stubGlobal('fetch', fetchMock);

    const result = await streamPlayground(
      {
        endpoint: 'chat-completions',
        messages: [{ content: 'Say hello', role: 'user' }],
        model: 'gpt-5.6-sol',
        projectID: 'prj_editor',
        reasoningEffort: 'medium',
        sessionID: 'chat-session-1',
      },
      onDelta,
      onReasoningDelta,
    );

    expect(onDelta).toHaveBeenCalledTimes(1);
    expect(onDelta).toHaveBeenCalledWith('Hello');
    expect(onReasoningDelta).toHaveBeenCalledWith('Checked');
    expect(result).toEqual({
      reasoning: 'Checked',
      text: 'Hello',
      usage: {
        cachedInputTokens: 4,
        inputTokens: 12,
        outputTokens: 7,
        reasoningOutputTokens: 3,
        totalTokens: 19,
      },
    });
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/chat/stream',
      expect.objectContaining({
        body: JSON.stringify({
          endpoint: 'chat-completions',
          messages: [{ content: 'Say hello', role: 'user' }],
          model: 'gpt-5.6-sol',
          project_id: 'prj_editor',
          reasoning_effort: 'medium',
          session_id: 'chat-session-1',
        }),
        credentials: 'same-origin',
        headers: { Accept: 'text/event-stream', 'Content-Type': 'application/json' },
        method: 'POST',
      }),
    );
  });

  it('omits project attribution when the dashboard usage bucket is selected', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        sseResponse([
          'data: {"choices":[{"delta":{"content":"Hello"}}]}\n\n',
          'data: {"choices":[],"usage":{"prompt_tokens":1,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens":1,"completion_tokens_details":{"reasoning_tokens":0},"total_tokens":2}}\n\ndata: [DONE]\n\n',
        ]),
      );
    vi.stubGlobal('fetch', fetchMock);

    await streamPlayground(
      {
        endpoint: 'chat-completions',
        messages: [{ content: 'Say hello', role: 'user' }],
        model: 'gpt-5.6-sol',
        reasoningEffort: 'medium',
        sessionID: 'chat-session-default',
      },
      vi.fn(),
    );

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/chat/stream',
      expect.objectContaining({
        body: JSON.stringify({
          endpoint: 'chat-completions',
          messages: [{ content: 'Say hello', role: 'user' }],
          model: 'gpt-5.6-sol',
          reasoning_effort: 'medium',
          session_id: 'chat-session-default',
        }),
      }),
    );
  });

  it('streams Responses events and reads detailed usage from response.completed', async () => {
    const onDelta = vi.fn();
    const onReasoningDelta = vi.fn();
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          sseResponse([
            'event: response.reasoning_summary_text.delta\ndata: {"type":"response.reasoning_summary_text.delta","delta":"Checked"}\n\n',
            'event: response.output_text.delta\ndata: {"type":"response.output_text.delta","delta":"Ready"}\n\n',
            'event: response.completed\ndata: {"type":"response.completed","response":{"usage":{"input_tokens":9,"input_tokens_details":{"cached_tokens":2},"output_tokens":6,"output_tokens_details":{"reasoning_tokens":5},"total_tokens":15}}}\n\n',
          ]),
        ),
    );

    await expect(
      streamPlayground(
        {
          endpoint: 'responses',
          messages: [
            { content: 'Be concise', role: 'system' },
            { content: 'Are you ready?', role: 'user' },
          ],
          model: 'gpt-5.6-terra',
          projectID: 'prj_editor',
          reasoningEffort: 'high',
          sessionID: 'chat-session-responses',
        },
        onDelta,
        onReasoningDelta,
      ),
    ).resolves.toEqual({
      reasoning: 'Checked',
      text: 'Ready',
      usage: {
        cachedInputTokens: 2,
        inputTokens: 9,
        outputTokens: 6,
        reasoningOutputTokens: 5,
        totalTokens: 15,
      },
    });
    expect(onDelta).toHaveBeenCalledWith('Ready');
    expect(onReasoningDelta).toHaveBeenCalledWith('Checked');
    expect(fetch).toHaveBeenCalledWith(
      '/api/chat/stream',
      expect.objectContaining({
        body: JSON.stringify({
          endpoint: 'responses',
          messages: [
            { content: 'Be concise', role: 'system' },
            { content: 'Are you ready?', role: 'user' },
          ],
          model: 'gpt-5.6-terra',
          project_id: 'prj_editor',
          reasoning_effort: 'high',
          session_id: 'chat-session-responses',
        }),
      }),
    );
  });

  it('marks edited transcripts as continuation restarts', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        sseResponse([
          'data: {"choices":[{"delta":{"content":"Rewritten"}}]}\n\n',
          'data: {"choices":[],"usage":{"prompt_tokens":3,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens":1,"completion_tokens_details":{"reasoning_tokens":0},"total_tokens":4}}\n\ndata: [DONE]\n\n',
        ]),
      );
    vi.stubGlobal('fetch', fetchMock);

    await streamPlayground(
      {
        endpoint: 'chat-completions',
        messages: [{ content: 'Edited prompt', role: 'user' }],
        model: 'gpt-5.6-luna',
        reasoningEffort: 'medium',
        restartSession: true,
        sessionID: 'chat-session-edited',
      },
      vi.fn(),
    );

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/chat/stream',
      expect.objectContaining({
        body: expect.stringContaining('"restart_session":true'),
      }),
    );
  });

  it('handles CRLF event boundaries split across network chunks', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          sseResponse([
            'data: {"choices":[{"delta":{"content":"OK"}}]}\r',
            '\n\r',
            '\ndata: {"choices":[],"usage":{"prompt_tokens":1,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens":1,"completion_tokens_details":{"reasoning_tokens":0},"total_tokens":2}}\r\n\r\n',
            'data: [DONE]\r\n\r\n',
          ]),
        ),
    );

    await expect(
      streamPlayground(
        {
          endpoint: 'chat-completions',
          messages: [{ content: 'OK?', role: 'user' }],
          model: 'gpt-5.6-sol',
          projectID: 'prj_editor',
          reasoningEffort: 'medium',
          sessionID: 'chat-session-crlf',
        },
        vi.fn(),
      ),
    ).resolves.toMatchObject({ text: 'OK', usage: { totalTokens: 2 } });
  });

  it('stops reading at the done sentinel after receiving terminal usage', async () => {
    const encoder = new TextEncoder();
    let cancelled = false;
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(
          encoder.encode(
            'data: {"choices":[],"usage":{"prompt_tokens":1,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens":1,"completion_tokens_details":{"reasoning_tokens":0},"total_tokens":2}}\n\ndata: [DONE]\n\n',
          ),
        );
      },
      cancel() {
        cancelled = true;
      },
    });
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(body)));

    await expect(
      streamPlayground(
        {
          endpoint: 'chat-completions',
          messages: [{ content: 'Done?', role: 'user' }],
          model: 'gpt-5.6-sol',
          reasoningEffort: 'medium',
          sessionID: 'chat-session-done',
        },
        vi.fn(),
      ),
    ).resolves.toMatchObject({ usage: { totalTokens: 2 } });
    expect(cancelled).toBe(true);
  });

  it('rejects invalid model lists, HTTP failures, stream failures, and missing terminal usage', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ data: [{ id: 12 }], object: 'list' }))
      .mockResolvedValueOnce(jsonResponse({ detail: 'That API key is invalid.' }, 401))
      .mockResolvedValueOnce(
        sseResponse(['data: {"error":{"message":"Codex could not complete the request."}}\n\n']),
      )
      .mockResolvedValueOnce(sseResponse(['data: {"choices":[{"delta":{"content":"Hi"}}]}\n\n']));
    vi.stubGlobal('fetch', fetchMock);

    await expect(listPlaygroundModels()).rejects.toThrow('invalid model list');
    await expect(listPlaygroundModels()).rejects.toThrow('That API key is invalid.');
    const request = {
      endpoint: 'chat-completions' as const,
      messages: [{ content: 'Hello', role: 'user' as const }],
      model: 'gpt-5.6-sol',
      projectID: 'prj_editor',
      reasoningEffort: 'medium' as const,
      sessionID: 'chat-session-errors',
    };
    await expect(streamPlayground(request, vi.fn())).rejects.toThrow(
      'Codex could not complete the request.',
    );
    await expect(streamPlayground(request, vi.fn())).rejects.toThrow('without terminal usage');
  });
});

function jsonResponse(body: object, status = 200): Response {
  return new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
    status,
  });
}

function sseResponse(chunks: string[]): Response {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
      controller.close();
    },
  });
  return new Response(body, { headers: { 'Content-Type': 'text/event-stream' } });
}
