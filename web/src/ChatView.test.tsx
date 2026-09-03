import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ChatView } from './ChatView';
import { isRecord } from './http';

describe('chat playground', () => {
  afterEach(() => {
    cleanup();
    sessionStorage.clear();
    vi.unstubAllGlobals();
  });

  it('uses project-scoped administrator chat and offers sample prompts', async () => {
    vi.stubGlobal('fetch', setupFetch());
    render(<ChatView />);

    const history = screen.getByRole('complementary', { name: 'Chat history' });
    const workspace = screen.getByRole('region', { name: 'Chat workspace' });

    expect(within(history).queryByLabelText('Project API key')).not.toBeInTheDocument();
    expect(within(history).getByText('No chats yet')).toBeVisible();
    expect(screen.queryByLabelText('Project API key')).not.toBeInTheDocument();
    expect(workspace).toContainElement(
      screen.getByRole('heading', { name: 'Ready to test the gateway?' }),
    );
    expect(workspace).toContainElement(screen.getByLabelText('Message'));
    expect(screen.getByLabelText('Message')).toHaveAttribute('rows', '2');
    expect(screen.queryByRole('textbox', { name: 'Instructions' })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /instructions/i }));
    expect(screen.getByRole('textbox', { name: 'Instructions' })).toBeVisible();

    expect((await screen.findAllByText('Editor')).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole('button', { name: 'Debug a stream' }));
    expect(screen.getByLabelText('Message')).toHaveValue(
      'Help me debug an SSE stream that stops before reporting terminal usage.',
    );
  });

  it('keeps conversations in memory while streaming a dashboard-counted request', async () => {
    const streamChunks = [
      'data: {"choices":[{"delta":{"content":"Hello"}}]}\n\n',
      'data: {"choices":[],"usage":{"prompt_tokens":12,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens":7,"completion_tokens_details":{"reasoning_tokens":3},"total_tokens":19}}\n\ndata: [DONE]\n\n',
    ];
    const fetchMock = setupFetch(
      vi.fn().mockImplementation(() => Promise.resolve(sseResponse(streamChunks))),
    );
    vi.stubGlobal('fetch', fetchMock);
    render(<ChatView />);

    expect(screen.getByRole('heading', { name: 'Ready to test the gateway?' })).toBeVisible();
    expect(screen.getByText('No chats yet')).toBeVisible();
    expect((await screen.findAllByText('gpt-5.6-sol')).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole('button', { name: /instructions/i }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Instructions' }), {
      target: { value: 'Be concise' },
    });
    const messageComposer = screen.getByLabelText('Message');
    fireEvent.change(messageComposer, { target: { value: 'Say hello' } });
    fireEvent.keyDown(messageComposer, { key: 'Enter', shiftKey: true });
    expect(streamRequestBodies(fetchMock)).toHaveLength(0);
    fireEvent.keyDown(messageComposer, { key: 'Enter' });

    expect(await screen.findByText('Hello')).toBeVisible();
    expect(
      screen.getByText(
        'Turn · 19 total · 12 input (4 cached, 8 uncached) · 7 output (3 reasoning)',
      ),
    ).toBeVisible();
    expect(screen.getByText('Conversation · 19 tokens · 1 turn · 4 cached')).toBeVisible();

    const conversation = screen.getByRole('log', { name: 'Conversation' });
    expect(conversation).toHaveClass('scroll-shadow', 'mx-auto', 'max-w-3xl', 'min-h-0');
    const workspace = screen.getByRole('region', { name: 'Chat workspace' });
    expect(workspace).toHaveClass('min-h-0');
    expect(workspace.parentElement).toHaveClass('overflow-hidden');
    expect(
      within(conversation).getByText('Say hello').closest('[data-slot="surface"]'),
    ).toHaveClass('rounded-2xl');

    Object.defineProperties(conversation, {
      clientHeight: { configurable: true, value: 300 },
      scrollHeight: { configurable: true, value: 1200 },
      scrollTop: { configurable: true, value: 100, writable: true },
    });
    fireEvent.scroll(conversation);
    const jumpButton = await screen.findByRole('button', { name: 'Jump to latest message' });
    fireEvent.click(jumpButton);
    expect(conversation.scrollTop).toBe(1200);

    const historyOption = screen.getByRole('button', { name: 'Open chat Say hello' });
    expect(historyOption).toBeVisible();
    const historyTitle = within(historyOption).getByText('Say hello');
    const historyModel = within(historyOption).getByText('gpt-5.6-sol');
    expect(historyTitle.parentElement).toBe(historyModel.parentElement);
    expect(historyTitle.parentElement).toHaveClass('flex', 'flex-col');
    expect(screen.queryByLabelText('Project API key')).not.toBeInTheDocument();
    const firstRequest = streamRequestBodies(fetchMock)[0];
    expect(firstRequest).toMatchObject({
      endpoint: 'chat-completions',
      messages: [
        { content: 'Be concise', role: 'system' },
        { content: 'Say hello', role: 'user' },
      ],
      model: 'gpt-5.6-sol',
      project_id: 'prj_editor',
      reasoning_effort: 'medium',
    });
    expect(firstRequest?.session_id).toMatch(/^chat-[0-9a-f-]{36}$/u);

    fireEvent.change(screen.getByLabelText('Message'), { target: { value: 'Follow up' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
    await waitFor(() => expect(streamRequestBodies(fetchMock)).toHaveLength(2));
    expect(await screen.findByText('Conversation · 38 tokens · 2 turns · 8 cached')).toBeVisible();
    const secondRequest = streamRequestBodies(fetchMock)[1];
    expect(secondRequest).toMatchObject({
      messages: [
        { content: 'Be concise', role: 'system' },
        { content: 'Say hello', role: 'user' },
        { content: 'Hello', role: 'assistant' },
        { content: 'Follow up', role: 'user' },
      ],
      session_id: firstRequest?.session_id,
    });

    fireEvent.click(screen.getByRole('button', { name: 'New chat' }));
    expect(screen.getByRole('heading', { name: 'Ready to test the gateway?' })).toBeVisible();
    expect(screen.queryByText('Hello')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Open chat Say hello' }));
    expect(await screen.findAllByText('Hello')).toHaveLength(2);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(5));
  });

  it('clears a submitted message before the response stream finishes', async () => {
    const pendingStream = openSseResponse([
      'data: {"choices":[{"delta":{"content":"### Live result\\n\\n- First item"}}]}\n\n',
    ]);
    const fetchMock = setupFetch(vi.fn().mockResolvedValue(pendingStream.response));
    vi.stubGlobal('fetch', fetchMock);
    render(<ChatView />);

    await screen.findAllByText('gpt-5.6-sol');
    const composer = screen.getByRole('textbox', { name: 'Message' });
    fireEvent.change(composer, { target: { value: 'Send immediately' } });
    fireEvent.keyDown(composer, { key: 'Enter' });

    expect(composer).toHaveValue('');
    expect(await screen.findByRole('heading', { name: 'Live result' })).toBeVisible();
    expect(
      within(screen.getByRole('log', { name: 'Conversation' })).getByRole('list'),
    ).toHaveTextContent('First item');
    expect(screen.queryByText(/### Live result/u)).not.toBeInTheDocument();
    expect(screen.queryByRole('status', { name: /Thinking/u })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Stop response' }));
    pendingStream.close();
  });

  it('keeps the composer focused and lets the user pause and resume streamed auto-scroll', async () => {
    const pendingStream = openSseResponse([]);
    vi.stubGlobal(
      'fetch',
      setupFetch(
        vi.fn().mockResolvedValue(pendingStream.response),
        modelListResponse,
        projectListResponse,
        persistedHistoryResponse,
      ),
    );
    render(<ChatView />);

    await screen.findByRole('button', { name: 'Open chat Remember this chat' });
    const composer = screen.getByRole('textbox', { name: 'Message' });
    const conversation = screen.getByRole('log', { name: 'Conversation' });
    const scrollTo = vi.fn(({ top }: ScrollToOptions) => {
      if (typeof top === 'number') conversation.scrollTop = top;
    });
    Object.defineProperties(conversation, {
      clientHeight: { configurable: true, value: 300 },
      scrollHeight: { configurable: true, value: 1200 },
      scrollTo: { configurable: true, value: scrollTo },
      scrollTop: { configurable: true, value: 900, writable: true },
    });

    fireEvent.change(composer, { target: { value: 'Follow the stream' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
    await waitFor(() => expect(composer).toHaveFocus());
    expect(screen.getByText('Generating response')).toBeVisible();

    conversation.scrollTop = 100;
    fireEvent.scroll(conversation);
    pendingStream.enqueue('data: {"choices":[{"delta":{"content":"A new token"}}]}\n\n');

    const jumpButton = await screen.findByRole('button', { name: 'Jump to latest message' });
    await waitFor(() => expect(jumpButton).toHaveTextContent('New response'));
    expect(conversation.scrollTop).toBe(100);

    fireEvent.click(jumpButton);
    expect(scrollTo).toHaveBeenCalledWith({ behavior: 'smooth', top: 1200 });
    expect(
      screen.queryByRole('button', { name: 'Jump to latest message' }),
    ).not.toBeInTheDocument();

    vi.stubGlobal(
      'matchMedia',
      vi.fn(() => ({ matches: true })),
    );
    conversation.scrollTop = 100;
    fireEvent.scroll(conversation);
    pendingStream.enqueue('data: {"choices":[{"delta":{"content":" again"}}]}\n\n');
    const reducedMotionJump = await screen.findByRole('button', {
      name: 'Jump to latest message',
    });
    await waitFor(() => expect(reducedMotionJump).toHaveTextContent('New response'));
    fireEvent.click(reducedMotionJump);
    expect(scrollTo).toHaveBeenLastCalledWith({ behavior: 'auto', top: 1200 });

    fireEvent.keyDown(composer, { key: 'Escape' });
    const streamCall = vi
      .mocked(fetch)
      .mock.calls.find((call) => String(call[0]) === '/api/chat/stream');
    expect((streamCall?.[1] as RequestInit | undefined)?.signal).toHaveProperty('aborted', true);
    pendingStream.close();
  });

  it('moves focus to the composer when a sample prompt is selected', async () => {
    vi.stubGlobal('fetch', setupFetch());
    render(<ChatView />);

    await screen.findAllByText('gpt-5.6-sol');
    fireEvent.click(screen.getByRole('button', { name: 'Draft a test plan' }));

    const composer = screen.getByRole('textbox', { name: 'Message' });
    expect(composer).toHaveValue(
      'Draft a focused test plan for an OpenAI-compatible chat endpoint.',
    );
    expect(composer).toHaveFocus();
  });

  it('restores chat history from the gateway after the view remounts', async () => {
    const fetchMock = setupFetch(vi.fn(), modelListResponse, projectListResponse, () =>
      jsonResponse({
        sessions: [
          {
            created_at: '2026-09-03T12:00:00Z',
            endpoint: 'chat-completions',
            id: 'chat-session-restored',
            instructions: 'Be concise.',
            messages: [
              { content: 'Remember this chat', role: 'user' },
              {
                content: 'Hello again',
                role: 'assistant',
                usage: {
                  cached_input_tokens: 1,
                  input_tokens: 4,
                  output_tokens: 2,
                  reasoning_output_tokens: 0,
                  total_tokens: 6,
                },
              },
            ],
            model: 'gpt-5.6-sol',
            project_id: 'prj_editor',
            reasoning_effort: 'medium',
            title: 'Remember this chat',
            updated_at: '2026-09-03T12:01:00Z',
          },
        ],
      }),
    );
    vi.stubGlobal('fetch', fetchMock);
    const firstRender = render(<ChatView />);

    expect(
      await screen.findByRole('button', { name: 'Open chat Remember this chat' }),
    ).toBeVisible();
    expect(
      within(screen.getByRole('log', { name: 'Conversation' })).getByText('Hello again'),
    ).toBeVisible();

    firstRender.unmount();
    render(<ChatView />);

    expect(
      await screen.findByRole('button', { name: 'Open chat Remember this chat' }),
    ).toBeVisible();
    const restoredConversation = screen.getByRole('log', { name: 'Conversation' });
    expect(within(restoredConversation).getByText('Remember this chat')).toBeVisible();
    expect(within(restoredConversation).getByText('Hello again')).toBeVisible();
    expect(
      fetchMock.mock.calls.filter((call) => String(call[0]) === '/api/chat/sessions'),
    ).toHaveLength(2);
    expect(sessionStorage.getItem('codex-gateway.chat-session.v1')).toBeNull();
  });

  it('renders streamed reasoning and Markdown as a restrained assistant turn', async () => {
    const fetchMock = setupFetch(
      vi
        .fn()
        .mockResolvedValue(
          sseResponse([
            'data: {"choices":[{"delta":{"reasoning_content":"**Inspecting the request**\\n\\nChecked the route."}}]}\n\n',
            'data: {"choices":[{"delta":{"content":"### Result\\n\\n- First"}}]}\n\n',
            'data: {"choices":[],"usage":{"prompt_tokens":12,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens":7,"completion_tokens_details":{"reasoning_tokens":3},"total_tokens":19}}\n\ndata: [DONE]\n\n',
          ]),
        ),
    );
    vi.stubGlobal('fetch', fetchMock);
    render(<ChatView />);

    await screen.findAllByText('gpt-5.6-sol');
    fireEvent.change(screen.getByLabelText('Message'), { target: { value: 'Explain it' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));

    expect(await screen.findByRole('heading', { name: 'Result' })).toBeVisible();
    const reasoning = screen.getByRole('button', {
      name: 'Reasoning summary: Inspecting the request',
    });
    expect(reasoning).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(reasoning);
    expect(screen.getByText('Checked the route.')).toBeVisible();
  });

  it('uses a real streamed reasoning heading for the active thinking state', async () => {
    const pendingStream = openSseResponse([
      'data: {"choices":[{"delta":{"reasoning_content":"**Checking the cache path**\\n\\nReviewing terminal usage."}}]}\n\n',
    ]);
    vi.stubGlobal('fetch', setupFetch(vi.fn().mockResolvedValue(pendingStream.response)));
    render(<ChatView />);

    await screen.findAllByText('gpt-5.6-sol');
    fireEvent.change(screen.getByLabelText('Message'), { target: { value: 'Inspect it' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));

    const spinner = await screen.findByRole('status', { name: 'Checking the cache path' });
    const activity = spinner.closest('[aria-live="polite"]');
    expect(activity).toHaveTextContent('Checking the cache path');
    expect(activity).toHaveTextContent('Reviewing terminal usage.');
    expect(activity).not.toHaveTextContent('**');

    fireEvent.click(screen.getByRole('button', { name: 'Stop response' }));
    pendingStream.close();
  });

  it('offers web search and renders the streamed tool activity with sources', async () => {
    const fetchMock = setupFetch(
      vi
        .fn()
        .mockResolvedValue(
          sseResponse([
            'event: gateway.web_search.started\ndata: {"type":"gateway.web_search.started","call_id":"web-call-1","query":"current release"}\n\n',
            'event: gateway.web_search.completed\ndata: {"type":"gateway.web_search.completed","call_id":"web-call-1","query":"current release","results":[{"type":"text_result","url":"https://example.test/release","title":"Release notes","snippet":"The current release."}]}\n\n',
            'data: {"choices":[{"delta":{"content":"Verified answer"}}]}\n\n',
            'data: {"choices":[],"usage":{"prompt_tokens":8,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens":3,"completion_tokens_details":{"reasoning_tokens":0},"total_tokens":11}}\n\ndata: [DONE]\n\n',
          ]),
        ),
    );
    vi.stubGlobal('fetch', fetchMock);
    render(<ChatView />);

    await screen.findAllByText('gpt-5.6-sol');
    const searchSwitch = screen.getByRole('switch', { name: 'Web search' });
    expect(searchSwitch).not.toBeChecked();
    fireEvent.click(searchSwitch);
    fireEvent.change(screen.getByLabelText('Message'), {
      target: { value: 'Find the current release' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));

    expect(await screen.findByText('Verified answer')).toBeVisible();
    const activity = screen.getByRole('button', { name: 'Searched the web: current release' });
    expect(activity).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(activity);
    const source = screen.getByRole('link', { name: 'Release notes' });
    expect(source).toHaveAttribute('href', 'https://example.test/release');
    expect(screen.getByText('The current release.')).toBeVisible();
    expect(streamRequestBodies(fetchMock)[0]).toMatchObject({ web_search: true });
  });

  it('opens a sandboxed HTML preview beside the chat and can maximize it', async () => {
    vi.stubGlobal(
      'fetch',
      setupFetch(vi.fn(), modelListResponse, projectListResponse, htmlHistoryResponse),
    );
    render(<ChatView />);

    fireEvent.click(await screen.findByRole('button', { name: 'Open HTML preview' }));

    const preview = screen.getByRole('region', { name: 'HTML preview' });
    const frame = within(preview).getByTitle('HTML preview');
    expect(frame).toHaveAttribute('sandbox', 'allow-scripts');
    expect(frame.getAttribute('srcdoc')).toContain('Content-Security-Policy');
    expect(frame.getAttribute('srcdoc')).toContain('<button>Play</button>');

    fireEvent.click(within(preview).getByRole('button', { name: 'Maximize HTML preview' }));
    expect(await screen.findByRole('dialog')).toHaveTextContent('HTML preview');
    expect(screen.getAllByTitle('HTML preview')).toHaveLength(2);
  });

  it('edits a user message by replacing later turns and restarting continuation state', async () => {
    const streamResponses = vi
      .fn()
      .mockResolvedValueOnce(
        sseResponse([
          'data: {"choices":[{"delta":{"content":"Original answer"}}]}\n\n',
          'data: {"choices":[],"usage":{"prompt_tokens":4,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens":2,"completion_tokens_details":{"reasoning_tokens":0},"total_tokens":6}}\n\ndata: [DONE]\n\n',
        ]),
      )
      .mockResolvedValueOnce(
        sseResponse([
          'data: {"choices":[{"delta":{"content":"Rewritten answer"}}]}\n\n',
          'data: {"choices":[],"usage":{"prompt_tokens":5,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens":2,"completion_tokens_details":{"reasoning_tokens":0},"total_tokens":7}}\n\ndata: [DONE]\n\n',
        ]),
      );
    const fetchMock = setupFetch(streamResponses);
    vi.stubGlobal('fetch', fetchMock);
    render(<ChatView />);

    await screen.findAllByText('gpt-5.6-sol');
    fireEvent.change(screen.getByLabelText('Message'), { target: { value: 'Original prompt' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
    expect(await screen.findByText('Original answer')).toBeVisible();

    fireEvent.click(screen.getByRole('button', { name: 'Edit message' }));
    const editor = screen.getByRole('textbox', { name: 'Edit message' });
    fireEvent.change(editor, { target: { value: 'Edited prompt' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save and regenerate' }));

    expect(await screen.findByText('Rewritten answer')).toBeVisible();
    expect(screen.queryByText('Original answer')).not.toBeInTheDocument();
    expect(streamRequestBodies(fetchMock)[1]).toMatchObject({
      messages: [{ content: 'Edited prompt', role: 'user' }],
      restart_session: true,
    });
  });

  it('renames and deletes an active persisted chat', async () => {
    const fetchMock = setupFetch(
      vi.fn(),
      modelListResponse,
      projectListResponse,
      persistedHistoryResponse,
    );
    vi.stubGlobal('fetch', fetchMock);
    render(<ChatView />);

    expect(
      await screen.findByRole('button', { name: 'Open chat Remember this chat' }),
    ).toBeVisible();
    fireEvent.click(screen.getByRole('button', { name: 'Actions for Remember this chat' }));
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Rename' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Chat title' }), {
      target: { value: 'Renamed chat' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(await screen.findByRole('button', { name: 'Open chat Renamed chat' })).toBeVisible();
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/chat/sessions/chat-session-restored',
      expect.objectContaining({ method: 'PATCH' }),
    );

    fireEvent.click(screen.getByRole('button', { name: 'Actions for Renamed chat' }));
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }));
    expect(screen.getByRole('alertdialog')).toBeVisible();
    fireEvent.click(screen.getByRole('button', { name: 'Delete chat' }));

    await waitFor(() => expect(screen.queryByText('Renamed chat')).not.toBeInTheDocument());
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/chat/sessions/chat-session-restored',
      expect.objectContaining({ method: 'DELETE' }),
    );
  });

  it('shows model-loading and streaming failures without losing the draft', async () => {
    let modelRequests = 0;
    const streamResponses = vi
      .fn()
      .mockResolvedValueOnce(
        sseResponse(['data: {"error":{"message":"Codex is unavailable."}}\n\n']),
      )
      .mockResolvedValueOnce(
        sseResponse([
          'data: {"choices":[{"delta":{"content":"Recovered"}}]}\n\n',
          'data: {"choices":[],"usage":{"prompt_tokens":3,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens":2,"completion_tokens_details":{"reasoning_tokens":0},"total_tokens":5}}\n\ndata: [DONE]\n\n',
        ]),
      );
    const fetchMock = setupFetch(streamResponses, () => {
      modelRequests += 1;
      return modelRequests === 1
        ? jsonResponse({ detail: 'Codex is unavailable.' }, 502)
        : modelListResponse();
    });
    vi.stubGlobal('fetch', fetchMock);
    render(<ChatView />);

    expect(await screen.findByText('Codex is unavailable.')).toBeVisible();

    fireEvent.click(screen.getByRole('button', { name: 'Retry setup' }));
    expect((await screen.findAllByText('gpt-5.6-sol')).length).toBeGreaterThan(0);
    fireEvent.change(screen.getByLabelText('Message'), { target: { value: 'Keep this draft' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));

    expect(await screen.findByText('Codex is unavailable.')).toBeVisible();
    expect(screen.getByLabelText('Message')).toHaveValue('Keep this draft');

    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
    expect(await screen.findByText('Recovered')).toBeVisible();
    expect(streamRequestBodies(fetchMock).at(-1)).toMatchObject({
      endpoint: 'chat-completions',
      messages: [{ content: 'Keep this draft', role: 'user' }],
      model: 'gpt-5.6-sol',
      project_id: 'prj_editor',
      reasoning_effort: 'medium',
      session_id: expect.stringMatching(/^chat-[0-9a-f-]{36}$/u),
    });
  });

  it('can send through the dashboard usage bucket when no projects exist', async () => {
    const streamResponses = vi
      .fn()
      .mockResolvedValue(
        sseResponse([
          'data: {"choices":[{"delta":{"content":"Hello"}}]}\n\n',
          'data: {"choices":[],"usage":{"prompt_tokens":1,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens":1,"completion_tokens_details":{"reasoning_tokens":0},"total_tokens":2}}\n\ndata: [DONE]\n\n',
        ]),
      );
    const fetchMock = setupFetch(streamResponses, modelListResponse, () =>
      jsonResponse({ projects: [] }),
    );
    vi.stubGlobal('fetch', fetchMock);
    render(<ChatView />);

    expect(await screen.findByText('Usage is counted in the Dashboard Chat bucket.')).toBeVisible();
    expect(screen.getByLabelText('Usage project')).toHaveTextContent('Dashboard Chat');
    fireEvent.change(screen.getByLabelText('Message'), { target: { value: 'Say hello' } });
    expect(screen.getByRole('button', { name: 'Send message' })).toBeEnabled();
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));

    expect(await screen.findByText('Hello')).toBeVisible();
    expect(streamRequestBodies(fetchMock)[0]).toMatchObject({
      endpoint: 'chat-completions',
      messages: [{ content: 'Say hello', role: 'user' }],
      model: 'gpt-5.6-sol',
      reasoning_effort: 'medium',
      session_id: expect.stringMatching(/^chat-[0-9a-f-]{36}$/u),
    });
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

function openSseResponse(chunks: string[]): {
  close: () => void;
  enqueue: (chunk: string) => void;
  response: Response;
} {
  const encoder = new TextEncoder();
  let streamController: ReadableStreamDefaultController<Uint8Array> | undefined;
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      streamController = controller;
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
    },
  });
  return {
    close: () => streamController?.close(),
    enqueue: (chunk) => streamController?.enqueue(encoder.encode(chunk)),
    response: new Response(body, { headers: { 'Content-Type': 'text/event-stream' } }),
  };
}

function modelListResponse(): Response {
  return jsonResponse({
    data: [{ created: 0, id: 'gpt-5.6-sol', object: 'model', owned_by: 'codex-gateway' }],
    object: 'list',
  });
}

function projectListResponse(): Response {
  return jsonResponse({
    projects: [
      { created_at: '2026-09-02T15:00:00Z', id: 'prj_editor', limits: [], name: 'Editor' },
    ],
  });
}

function persistedHistoryResponse(): Response {
  return jsonResponse({
    sessions: [
      {
        created_at: '2026-09-03T12:00:00Z',
        endpoint: 'chat-completions',
        id: 'chat-session-restored',
        instructions: '',
        messages: [
          { content: 'Remember this chat', role: 'user' },
          {
            content: 'Remembered',
            role: 'assistant',
            usage: {
              cached_input_tokens: 0,
              input_tokens: 4,
              output_tokens: 2,
              reasoning_output_tokens: 0,
              total_tokens: 6,
            },
          },
        ],
        model: 'gpt-5.6-sol',
        project_id: 'prj_editor',
        reasoning_effort: 'medium',
        title: 'Remember this chat',
        updated_at: '2026-09-03T12:01:00Z',
      },
    ],
  });
}

function htmlHistoryResponse(): Response {
  return jsonResponse({
    sessions: [
      {
        created_at: '2026-09-03T12:00:00Z',
        endpoint: 'chat-completions',
        id: 'chat-session-html',
        instructions: '',
        messages: [
          { content: 'Build a button', role: 'user' },
          {
            content: '```html\n<!doctype html><button>Play</button>\n```',
            role: 'assistant',
            usage: {
              cached_input_tokens: 0,
              input_tokens: 4,
              output_tokens: 8,
              reasoning_output_tokens: 0,
              total_tokens: 12,
            },
          },
        ],
        model: 'gpt-5.6-sol',
        project_id: 'prj_editor',
        reasoning_effort: 'medium',
        title: 'HTML button',
        updated_at: '2026-09-03T12:01:00Z',
      },
    ],
  });
}

function setupFetch(
  streamResponses = vi.fn(),
  modelsResponse: () => Response = modelListResponse,
  projectsResponse: () => Response = projectListResponse,
  historyResponse: () => Response = () => jsonResponse({ sessions: [] }),
) {
  return vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input);
    if (path === '/api/chat/models') return Promise.resolve(modelsResponse());
    if (path === '/api/projects') return Promise.resolve(projectsResponse());
    if (path === '/api/chat/sessions') return Promise.resolve(historyResponse());
    if (path.startsWith('/api/chat/sessions/') && init?.method === 'PATCH') {
      return Promise.resolve(new Response(null, { status: 204 }));
    }
    if (path.startsWith('/api/chat/sessions/') && init?.method === 'DELETE') {
      return Promise.resolve(new Response(null, { status: 204 }));
    }
    if (path === '/api/chat/stream') return streamResponses();
    return Promise.reject(new Error(`Unexpected request: ${path}`));
  });
}

function streamRequestBodies(fetchMock: ReturnType<typeof vi.fn>): Record<string, unknown>[] {
  const bodies: Record<string, unknown>[] = [];
  for (const call of fetchMock.mock.calls) {
    if (String(call[0]) !== '/api/chat/stream') continue;
    const options: unknown = call[1];
    if (!isRecord(options) || typeof options.body !== 'string') {
      throw new Error('Chat stream request omitted a JSON body.');
    }
    const body: unknown = JSON.parse(options.body);
    if (!isRecord(body)) throw new Error('Chat stream request body was not an object.');
    bodies.push(body);
  }
  return bodies;
}
