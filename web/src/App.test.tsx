import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const firebaseClient = vi.hoisted(() => ({
  requestGoogleIDToken: vi.fn<() => Promise<string>>(),
  signOutFromGoogle: vi.fn<() => Promise<void>>(),
}));

vi.mock('./firebase-client', () => firebaseClient);

import { App } from './App';

describe('dashboard shell', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    firebaseClient.requestGoogleIDToken.mockResolvedValue('signed-firebase-id-token');
    firebaseClient.signOutFromGoogle.mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    window.history.replaceState({}, '', '/');
  });

  it('presents the HeroUI gateway overview and API documentation to an administrator', async () => {
    mockFetch(
      jsonResponse({ authenticated: true, setup_required: false }),
      jsonResponse({ connected: false, requires_openai_auth: true }),
    );
    const { container } = render(<App />);

    expect(
      await screen.findByRole('heading', { level: 1, name: /gateway overview/i }),
    ).toBeVisible();
    expect(screen.getByText('Gateway online')).toBeVisible();
    expect(screen.getByText('Codex account')).toBeVisible();
    expect(screen.getByRole('link', { name: /api reference/i })).toHaveAttribute('href', '/doc');
    expect(container.querySelector('.card')).toBeInTheDocument();
    expect(container.querySelector('.chip')).toBeInTheDocument();
    expect(container.querySelector('.progress-bar')).toBeInTheDocument();
    expect(container.querySelector('.metric-grid')).not.toBeInTheDocument();

    const iconLabels = [
      screen.getByLabelText('Codex Gateway home'),
      screen.getByRole('tab', { name: 'Overview' }),
      screen.getByRole('link', { name: /api reference/i }),
    ];

    for (const element of iconLabels) {
      expect(element.firstElementChild).toHaveClass('inline-flex', 'items-center', 'gap-2');
    }

    expect(screen.getByLabelText('Codex Gateway home').querySelector('p')).not.toBeInTheDocument();
    expect(
      screen.getByRole('tab', { name: 'Overview' }).querySelector('p'),
    ).not.toBeInTheDocument();

    const headerRow = screen.getByLabelText('Codex Gateway home').parentElement;
    expect(headerRow).toContainElement(screen.getByRole('tablist', { name: 'Gateway navigation' }));
    expect(headerRow).toContainElement(screen.getByRole('button', { name: 'Sign out' }));
    expect(screen.getByRole('tab', { name: 'API keys' }).firstElementChild).toHaveClass(
      'whitespace-nowrap',
    );
    expect(screen.getByRole('tab', { name: 'Chat' })).toBeVisible();
    expect(screen.getAllByRole('tab')[0]).toHaveAccessibleName('Chat');

    const dashboardMain = screen.getByRole('main');
    expect(dashboardMain).toHaveClass('w-full', 'min-w-0');
    expect(screen.getByRole('tabpanel')).toHaveClass('w-full', 'min-w-0');

    fireEvent.click(screen.getByRole('tab', { name: 'Chat' }));
    expect(dashboardMain).toHaveClass('max-w-[1800px]');
    expect(
      await screen.findByRole('heading', { name: 'Ready to test the gateway?' }),
    ).toBeVisible();
    expect(screen.getByText('No chats yet')).toBeVisible();
  });

  it('uses accessible tabs and loads project data only after the Projects tab is opened', async () => {
    const fetchMock = mockFetch(
      jsonResponse({ authenticated: true, setup_required: false }),
      jsonResponse({ connected: true, plan_type: 'plus', requires_openai_auth: true }),
      jsonResponse({
        projects: [
          { created_at: '2026-09-02T15:00:00Z', id: 'prj_1', limits: [], name: 'OpenCode' },
        ],
      }),
    );
    render(<App />);

    expect(await screen.findByRole('tab', { name: 'Overview' })).toHaveAttribute(
      'aria-selected',
      'true',
    );
    expect(fetchMock).toHaveBeenCalledTimes(2);

    fireEvent.click(screen.getByRole('tab', { name: 'Projects' }));

    expect(await screen.findByRole('heading', { name: 'Projects' })).toBeVisible();
    expect(await screen.findByText('OpenCode')).toBeVisible();
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/projects',
      expect.objectContaining({ credentials: 'same-origin' }),
    );
  });

  it('uses stable paths for tabs and follows browser history navigation', async () => {
    window.history.replaceState({}, '', '/usage');
    mockFetch(
      jsonResponse({ authenticated: true, setup_required: false }),
      jsonResponse({
        api_keys: [],
        cached_input_tokens: 0,
        input_tokens: 0,
        output_tokens: 0,
        period_end: '2026-10-01T00:00:00Z',
        period_start: '2026-09-01T00:00:00Z',
        projects: [],
        reasoning_output_tokens: 0,
        requests: 0,
        total_tokens: 0,
      }),
    );
    render(<App />);

    expect(await screen.findByRole('tab', { name: 'Usage' })).toHaveAttribute(
      'aria-selected',
      'true',
    );
    expect(await screen.findByRole('heading', { name: 'Usage' })).toBeVisible();
    expect(window.location.pathname).toBe('/usage');

    fireEvent.click(screen.getByRole('tab', { name: 'Overview' }));
    expect(window.location.pathname).toBe('/overview');
    expect(
      await screen.findByRole('heading', { level: 1, name: /gateway overview/i }),
    ).toBeVisible();

    window.history.pushState({}, '', '/settings');
    window.dispatchEvent(new PopStateEvent('popstate'));
    expect(await screen.findByRole('tab', { name: 'Settings' })).toHaveAttribute(
      'aria-selected',
      'true',
    );
    expect(await screen.findByRole('heading', { name: 'Settings' })).toBeVisible();
    expect(screen.getByLabelText('Codex Gateway home')).toHaveAttribute('href', '/overview');
  });

  it('renders the interactive API reference at /doc', async () => {
    const fetchMock = mockFetch();
    window.history.replaceState({}, '', '/doc');

    render(<App />);

    expect(screen.getByText('Loading OpenAPI documentation…')).toBeVisible();
    await waitFor(() => expect(document.querySelector('.swagger-ui')).toBeInTheDocument());
    expect(document.querySelector('.docs-header')).not.toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalledWith('/api/auth/status', expect.anything());
  });

  it('signs an approved Google administrator in and opens the dashboard', async () => {
    const fetchMock = mockFetch(
      jsonResponse({ authenticated: false }),
      jsonResponse({
        api_key: 'test-api-key',
        app_id: 'test-app-id',
        auth_domain: 'test.firebaseapp.com',
        project_id: 'test-project',
      }),
      jsonResponse({
        authenticated: true,
        email: 'administrator@example.test',
        expires_at: '2026-09-03T12:00:00Z',
        role: 'administrator',
      }),
      jsonResponse({ connected: false, requires_openai_auth: true }),
    );
    render(<App />);

    expect(await screen.findByRole('heading', { name: 'Sign in to Codex Gateway' })).toBeVisible();
    expect(screen.queryByLabelText('Password')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Continue with Google' }));

    expect(
      await screen.findByRole('heading', { level: 1, name: /gateway overview/i }),
    ).toBeVisible();
    expect(firebaseClient.requestGoogleIDToken).toHaveBeenCalledWith({
      apiKey: 'test-api-key',
      appId: 'test-app-id',
      authDomain: 'test.firebaseapp.com',
      projectId: 'test-project',
    });
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/auth/google',
      expect.objectContaining({
        body: JSON.stringify({ id_token: 'signed-firebase-id-token' }),
        credentials: 'same-origin',
        method: 'POST',
      }),
    );
  });

  it('shows an allowlist rejection and lets the user choose another Google account', async () => {
    const fetchMock = mockFetch(
      jsonResponse({ authenticated: false }),
      jsonResponse({
        api_key: 'test-api-key',
        app_id: 'test-app-id',
        auth_domain: 'test.firebaseapp.com',
        project_id: 'test-project',
      }),
      jsonResponse({ detail: 'This Google account is not approved for this gateway.' }, 403),
    );
    render(<App />);

    fireEvent.click(await screen.findByRole('button', { name: 'Continue with Google' }));

    expect(
      await screen.findByText('This Google account is not approved for this gateway.'),
    ).toBeVisible();
    expect(firebaseClient.signOutFromGoogle).toHaveBeenCalledOnce();
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it('reports a cancelled Google popup without attempting a token exchange', async () => {
    firebaseClient.requestGoogleIDToken.mockRejectedValue(
      new Error('Google sign-in was cancelled.'),
    );
    const fetchMock = mockFetch(
      jsonResponse({ authenticated: false }),
      jsonResponse({
        api_key: 'test-api-key',
        app_id: 'test-app-id',
        auth_domain: 'test.firebaseapp.com',
        project_id: 'test-project',
      }),
    );
    render(<App />);

    fireEvent.click(await screen.findByRole('button', { name: 'Continue with Google' }));

    expect(await screen.findByText('Google sign-in was cancelled.')).toBeVisible();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('shows an authentication status failure and retries it', async () => {
    const fetchMock = vi
      .fn()
      .mockRejectedValueOnce(new Error('Gateway is offline.'))
      .mockResolvedValueOnce(jsonResponse({ authenticated: false }));
    vi.stubGlobal('fetch', fetchMock);
    render(<App />);

    expect(await screen.findByText('Gateway is offline.')).toBeVisible();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await screen.findByRole('heading', { name: 'Sign in to Codex Gateway' })).toBeVisible();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('signs the administrator out and returns to the login screen', async () => {
    const fetchMock = mockFetch(
      jsonResponse({ authenticated: true, setup_required: false }),
      jsonResponse({ connected: false, requires_openai_auth: true }),
      jsonResponse({ authenticated: false }),
    );
    render(<App />);

    fireEvent.click(await screen.findByRole('button', { name: 'Sign out' }));

    expect(await screen.findByRole('heading', { name: 'Sign in to Codex Gateway' })).toBeVisible();
    expect(firebaseClient.signOutFromGoogle).toHaveBeenCalledOnce();
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/auth/logout',
      expect.objectContaining({ credentials: 'same-origin', method: 'POST' }),
    );
  });

  it('starts container-safe Codex device login and refreshes the account', async () => {
    const fetchMock = mockFetch(
      jsonResponse({ authenticated: true, setup_required: false }),
      jsonResponse({ connected: false, requires_openai_auth: true }),
      jsonResponse(
        {
          login_id: 'login-123',
          user_code: 'ABCD-1234',
          verification_url: 'https://auth.openai.com/codex/device',
        },
        201,
      ),
      jsonResponse({
        connected: true,
        email: 'alex@example.test',
        plan_type: 'plus',
        requires_openai_auth: true,
        type: 'chatgpt',
      }),
    );
    render(<App />);

    fireEvent.click(await screen.findByRole('button', { name: 'Connect account' }));

    expect(await screen.findByRole('heading', { name: 'Connect ChatGPT' })).toBeVisible();
    expect(screen.getByText('ABCD-1234')).toBeVisible();
    expect(screen.getByRole('link', { name: 'Open OpenAI sign in' })).toHaveAttribute(
      'href',
      'https://auth.openai.com/codex/device',
    );
    fireEvent.click(screen.getByRole('button', { name: 'Check connection' }));

    expect(await screen.findByText('alex@example.test')).toBeVisible();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/codex/login/device',
      expect.objectContaining({ credentials: 'same-origin', method: 'POST' }),
    );
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
