import { Alert, Button, Card, Chip, Link, Spinner, Typography } from '@heroui/react';
import { useState } from 'react';
import { LockKeyhole, Radio, ShieldCheck } from 'lucide-react';

import { authenticateWithGoogleToken, readFirebaseConfig } from './auth-api';
import { requestGoogleIDToken, signOutFromGoogle } from './firebase-client';

interface AdministratorAccessProps {
  onAuthenticated: () => void;
}

export function AdministratorAccess({ onAuthenticated }: AdministratorAccessProps) {
  const [error, setError] = useState<string>();
  const [submitting, setSubmitting] = useState(false);

  async function handleGoogleSignIn() {
    setError(undefined);
    setSubmitting(true);
    try {
      const configuration = await readFirebaseConfig();
      const token = await requestGoogleIDToken(configuration);
      await authenticateWithGoogleToken(token);
      onAuthenticated();
    } catch (cause) {
      try {
        await signOutFromGoogle();
      } catch {
        // No local session was created, so a Firebase sign-out failure cannot grant access.
      }
      setError(cause instanceof Error ? cause.message : 'Unable to sign in with Google.');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="grid min-h-screen place-items-center bg-background px-5 py-10 text-foreground">
      <div className="flex w-full max-w-md flex-col gap-6">
        <Link aria-label="Codex Gateway home" href="/">
          <span className="inline-flex items-center gap-2">
            <Radio aria-hidden="true" size={20} strokeWidth={2.25} />
            <span className="font-semibold">Codex Gateway</span>
          </span>
        </Link>

        <Card variant="default">
          <Card.Header>
            <div className="flex items-start gap-3">
              <LockKeyhole aria-hidden="true" size={22} />
              <div className="flex flex-col gap-2">
                <Card.Title>Sign in to Codex Gateway</Card.Title>
                <Card.Description>
                  Continue with an approved Google account to manage this local gateway.
                </Card.Description>
              </div>
            </div>
          </Card.Header>
          <Card.Content className="flex flex-col gap-5">
            {error ? (
              <Alert status="danger">
                <Alert.Indicator />
                <Alert.Content>
                  <Alert.Title>Sign-in failed</Alert.Title>
                  <Alert.Description>{error}</Alert.Description>
                </Alert.Content>
              </Alert>
            ) : null}

            <Button
              fullWidth
              isDisabled={submitting}
              variant="tertiary"
              onPress={handleGoogleSignIn}
            >
              {submitting ? <Spinner color="current" size="sm" /> : <ShieldCheck aria-hidden />}
              {submitting ? 'Waiting for Google' : 'Continue with Google'}
            </Button>

            <div className="flex flex-col items-start gap-2">
              <Chip color="success" size="sm" variant="soft">
                <Chip.Label>Approved accounts only</Chip.Label>
              </Chip>
              <Typography color="muted" type="body-xs">
                Access is limited to the administrator and emails explicitly listed in the gateway
                configuration.
              </Typography>
            </div>
          </Card.Content>
        </Card>

        <div className="flex items-center justify-between gap-4">
          <Typography color="muted" type="body-xs">
            Local access only by default
          </Typography>
          <Link href="/doc">
            API reference
            <Link.Icon />
          </Link>
        </div>
      </div>
    </main>
  );
}
