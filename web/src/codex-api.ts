import { isRecord, readJSONResponse } from './http';

export interface CodexAccountStatus {
  connected: boolean;
  email?: string;
  planType?: string;
  requiresOpenAIAuth: boolean;
  type?: string;
}

export interface CodexDeviceLogin {
  loginID: string;
  userCode: string;
  verificationURL: string;
}

export async function readCodexAccount(signal?: AbortSignal): Promise<CodexAccountStatus> {
  const response = await fetch('/api/codex/account', {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
    signal: signal ?? null,
  });
  const body = await readJSONResponse(response);
  if (
    !isRecord(body) ||
    typeof body.connected !== 'boolean' ||
    typeof body.requires_openai_auth !== 'boolean'
  ) {
    throw new Error('The gateway returned an invalid Codex account status.');
  }
  const status: CodexAccountStatus = {
    connected: body.connected,
    requiresOpenAIAuth: body.requires_openai_auth,
  };
  if (typeof body.email === 'string') status.email = body.email;
  if (typeof body.plan_type === 'string') status.planType = body.plan_type;
  if (typeof body.type === 'string') status.type = body.type;
  return status;
}

export async function startCodexDeviceLogin(): Promise<CodexDeviceLogin> {
  const response = await fetch('/api/codex/login/device', {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
    method: 'POST',
  });
  const body = await readJSONResponse(response);
  if (
    !isRecord(body) ||
    typeof body.login_id !== 'string' ||
    typeof body.user_code !== 'string' ||
    typeof body.verification_url !== 'string'
  ) {
    throw new Error('The gateway returned invalid Codex login instructions.');
  }
  const verificationURL = new URL(body.verification_url);
  if (verificationURL.protocol !== 'https:') {
    throw new Error('The gateway returned an unsafe Codex verification URL.');
  }
  return {
    loginID: body.login_id,
    userCode: body.user_code,
    verificationURL: verificationURL.toString(),
  };
}

export async function cancelCodexLogin(loginID: string): Promise<void> {
  const response = await fetch('/api/codex/login/cancel', {
    body: JSON.stringify({ login_id: loginID }),
    credentials: 'same-origin',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    method: 'POST',
  });
  await readJSONResponse(response);
}

export async function logoutCodexAccount(): Promise<void> {
  const response = await fetch('/api/codex/logout', {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
    method: 'POST',
  });
  await readJSONResponse(response);
}
