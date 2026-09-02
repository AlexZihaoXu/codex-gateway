import { isRecord, readJSONResponse } from './http';

export interface AuthenticationStatus {
  authenticated: boolean;
  email?: string;
  role?: 'administrator' | 'operator';
}

export interface FirebaseConfiguration {
  apiKey: string;
  authDomain: string;
  projectId: string;
  appId: string;
}

export async function readAuthenticationStatus(
  signal?: AbortSignal,
): Promise<AuthenticationStatus> {
  const response = await fetch('/api/auth/status', {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
    signal: signal ?? null,
  });
  const body = await readJSONResponse(response);
  if (
    !isRecord(body) ||
    typeof body.authenticated !== 'boolean' ||
    (body.email !== undefined && typeof body.email !== 'string') ||
    (body.role !== undefined && body.role !== 'administrator' && body.role !== 'operator')
  ) {
    throw new Error('The gateway returned an invalid authentication status.');
  }
  return {
    authenticated: body.authenticated,
    ...(typeof body.email === 'string' ? { email: body.email } : {}),
    ...(body.role === 'administrator' || body.role === 'operator' ? { role: body.role } : {}),
  };
}

export async function readFirebaseConfig(): Promise<FirebaseConfiguration> {
  const response = await fetch('/api/auth/config', {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
  });
  const body = await readJSONResponse(response);
  if (
    !isRecord(body) ||
    typeof body.api_key !== 'string' ||
    typeof body.auth_domain !== 'string' ||
    typeof body.project_id !== 'string' ||
    typeof body.app_id !== 'string'
  ) {
    throw new Error('The gateway returned an invalid Firebase configuration.');
  }
  return {
    apiKey: body.api_key,
    authDomain: body.auth_domain,
    projectId: body.project_id,
    appId: body.app_id,
  };
}

export async function authenticateWithGoogleToken(idToken: string): Promise<void> {
  const response = await fetch('/api/auth/google', {
    body: JSON.stringify({ id_token: idToken }),
    credentials: 'same-origin',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    method: 'POST',
  });
  await readJSONResponse(response);
}

export async function logoutAdministrator(): Promise<void> {
  const response = await fetch('/api/auth/logout', {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
    method: 'POST',
  });
  await readJSONResponse(response);
}
