import { Alert, Button, Spinner } from '@heroui/react';
import { type ReactNode, useEffect, useState } from 'react';

import { AdministratorAccess } from './AdministratorAccess';
import { logoutAdministrator, readAuthenticationStatus } from './auth-api';
import { signOutFromGoogle } from './firebase-client';

type AuthenticationState =
  { kind: 'error'; message: string } | { kind: 'loading' } | { kind: 'login' } | { kind: 'ready' };

interface AuthenticationGateProps {
  children: (signOut: () => Promise<void>) => ReactNode;
}

export function AuthenticationGate({ children }: AuthenticationGateProps) {
  const [attempt, setAttempt] = useState(0);
  const [state, setState] = useState<AuthenticationState>({ kind: 'loading' });

  useEffect(() => {
    const controller = new AbortController();
    readAuthenticationStatus(controller.signal)
      .then((status) => {
        setState({ kind: status.authenticated ? 'ready' : 'login' });
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted) {
          setState({
            kind: 'error',
            message: cause instanceof Error ? cause.message : 'Unable to reach the gateway.',
          });
        }
      });
    return () => controller.abort();
  }, [attempt]);

  async function signOut() {
    try {
      await logoutAdministrator();
      try {
        await signOutFromGoogle();
      } catch {
        // The HTTP-only gateway session is already revoked, so the dashboard remains locked.
      }
      setState({ kind: 'login' });
    } catch (cause) {
      setState({
        kind: 'error',
        message: cause instanceof Error ? cause.message : 'Unable to sign out.',
      });
    }
  }

  if (state.kind === 'ready') {
    return children(signOut);
  }
  if (state.kind === 'login') {
    return <AdministratorAccess onAuthenticated={() => setState({ kind: 'ready' })} />;
  }
  if (state.kind === 'error') {
    return (
      <main className="grid min-h-screen place-items-center bg-background p-6 text-foreground">
        <div className="w-full max-w-lg">
          <Alert status="danger">
            <Alert.Indicator />
            <Alert.Content>
              <Alert.Title>Gateway unavailable</Alert.Title>
              <Alert.Description>{state.message}</Alert.Description>
            </Alert.Content>
            <Button
              size="sm"
              variant="secondary"
              onPress={() => {
                setState({ kind: 'loading' });
                setAttempt((value) => value + 1);
              }}
            >
              Retry
            </Button>
          </Alert>
        </div>
      </main>
    );
  }
  return (
    <main className="grid min-h-screen place-items-center bg-background text-foreground">
      <div className="flex items-center gap-3" role="status">
        <Spinner size="sm" />
        <span>Checking administrator session...</span>
      </div>
    </main>
  );
}
