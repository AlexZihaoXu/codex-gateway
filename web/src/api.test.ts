import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  authenticateWithGoogleToken,
  logoutAdministrator,
  readAuthenticationStatus,
  readFirebaseConfig,
} from './auth-api';
import {
  cancelCodexLogin,
  logoutCodexAccount,
  readCodexAccount,
  startCodexDeviceLogin,
} from './codex-api';
import { isRecord, readJSONResponse } from './http';

describe('gateway API clients', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('maps authentication settings and submits Google session actions', async () => {
    const fetchMock = mockFetch(
      jsonResponse({ authenticated: false }),
      jsonResponse({
        api_key: 'test-api-key',
        app_id: 'test-app-id',
        auth_domain: 'test.firebaseapp.com',
        project_id: 'test-project',
      }),
      jsonResponse({ authenticated: true }),
      jsonResponse({ authenticated: false }),
    );

    await expect(readAuthenticationStatus()).resolves.toEqual({
      authenticated: false,
    });
    await expect(readFirebaseConfig()).resolves.toEqual({
      apiKey: 'test-api-key',
      appId: 'test-app-id',
      authDomain: 'test.firebaseapp.com',
      projectId: 'test-project',
    });
    await authenticateWithGoogleToken('signed-token');
    await logoutAdministrator();

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/api/auth/status',
      expect.objectContaining({ signal: null }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/auth/google',
      expect.objectContaining({
        body: JSON.stringify({ id_token: 'signed-token' }),
        method: 'POST',
      }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      4,
      '/api/auth/logout',
      expect.objectContaining({ credentials: 'same-origin', method: 'POST' }),
    );
  });

  it('rejects malformed authentication status', async () => {
    mockFetch(jsonResponse({ authenticated: 'yes' }));

    await expect(readAuthenticationStatus()).rejects.toThrow(
      'The gateway returned an invalid authentication status.',
    );
  });

  it('rejects incomplete Firebase client configuration', async () => {
    mockFetch(jsonResponse({ api_key: 'test-api-key', project_id: 'test-project' }));

    await expect(readFirebaseConfig()).rejects.toThrow(
      'The gateway returned an invalid Firebase configuration.',
    );
  });

  it('maps complete and minimal Codex account responses', async () => {
    mockFetch(
      jsonResponse({
        connected: true,
        email: 'admin@example.test',
        plan_type: 'plus',
        requires_openai_auth: true,
        type: 'chatgpt',
      }),
      jsonResponse({ connected: false, requires_openai_auth: false }),
    );

    await expect(readCodexAccount()).resolves.toEqual({
      connected: true,
      email: 'admin@example.test',
      planType: 'plus',
      requiresOpenAIAuth: true,
      type: 'chatgpt',
    });
    await expect(readCodexAccount()).resolves.toEqual({
      connected: false,
      requiresOpenAIAuth: false,
    });
  });

  it('rejects malformed Codex account and device-login responses', async () => {
    mockFetch(
      jsonResponse({ connected: false }),
      jsonResponse({ login_id: 'login-1', verification_url: 'https://example.test' }),
      jsonResponse({
        login_id: 'login-1',
        user_code: 'CODE-123',
        verification_url: 'http://example.test/device',
      }),
    );

    await expect(readCodexAccount()).rejects.toThrow(
      'The gateway returned an invalid Codex account status.',
    );
    await expect(startCodexDeviceLogin()).rejects.toThrow(
      'The gateway returned invalid Codex login instructions.',
    );
    await expect(startCodexDeviceLogin()).rejects.toThrow(
      'The gateway returned an unsafe Codex verification URL.',
    );
  });

  it('normalizes safe login instructions and sends cancellation identifiers', async () => {
    const fetchMock = mockFetch(
      jsonResponse({
        login_id: 'login-1',
        user_code: 'CODE-123',
        verification_url: 'https://example.test/device',
      }),
      jsonResponse({}),
      jsonResponse({ status: 'ok' }),
    );

    await expect(startCodexDeviceLogin()).resolves.toEqual({
      loginID: 'login-1',
      userCode: 'CODE-123',
      verificationURL: 'https://example.test/device',
    });
    await cancelCodexLogin('login-1');
    await logoutCodexAccount();

    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/codex/login/cancel',
      expect.objectContaining({ body: JSON.stringify({ login_id: 'login-1' }), method: 'POST' }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/codex/logout',
      expect.objectContaining({ credentials: 'same-origin', method: 'POST' }),
    );
  });
});

describe('JSON response handling', () => {
  it('returns successful JSON and recognizes only non-null objects', async () => {
    await expect(readJSONResponse(jsonResponse({ ok: true }))).resolves.toEqual({ ok: true });
    expect(isRecord({})).toBe(true);
    expect(isRecord(null)).toBe(false);
    expect(isRecord('value')).toBe(false);
  });

  it('uses an API problem detail when available', async () => {
    await expect(readJSONResponse(jsonResponse({ detail: 'Access denied.' }, 403))).rejects.toThrow(
      'Access denied.',
    );
  });

  it('falls back to the response status when the error body is unusable', async () => {
    await expect(readJSONResponse(new Response('not JSON', { status: 502 }))).rejects.toThrow(
      'The gateway request failed with status 502.',
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
