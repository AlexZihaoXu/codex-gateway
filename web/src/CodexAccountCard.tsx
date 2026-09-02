import {
  Alert,
  AlertDialog,
  Button,
  Card,
  Chip,
  Link,
  Modal,
  Spinner,
  Surface,
  Typography,
} from '@heroui/react';
import {
  CloudOff,
  ExternalLink,
  LogOut,
  RefreshCw,
  ShieldAlert,
  UserRoundCheck,
} from 'lucide-react';
import { useEffect, useState } from 'react';

import {
  cancelCodexLogin,
  type CodexAccountStatus,
  type CodexDeviceLogin,
  logoutCodexAccount,
  readCodexAccount,
  startCodexDeviceLogin,
} from './codex-api';

type AccountState =
  | { kind: 'connected'; status: CodexAccountStatus }
  | { kind: 'disconnected' }
  | { kind: 'error'; message: string }
  | { kind: 'loading' };

export function CodexAccountCard({ allowDisconnect = false }: { allowDisconnect?: boolean }) {
  const [account, setAccount] = useState<AccountState>({ kind: 'loading' });
  const [login, setLogin] = useState<CodexDeviceLogin>();
  const [loginError, setLoginError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [disconnectOpen, setDisconnectOpen] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    readCodexAccount(controller.signal)
      .then((status) => setAccount(accountState(status)))
      .catch((cause: unknown) => {
        if (!controller.signal.aborted) {
          setAccount({
            kind: 'error',
            message: errorMessage(cause, 'Unable to read the Codex account.'),
          });
        }
      });
    return () => controller.abort();
  }, []);

  async function startLogin() {
    setBusy(true);
    setLoginError(undefined);
    try {
      setLogin(await startCodexDeviceLogin());
    } catch (cause) {
      setLoginError(errorMessage(cause, 'Unable to start Codex login.'));
    } finally {
      setBusy(false);
    }
  }

  async function checkConnection() {
    setBusy(true);
    setLoginError(undefined);
    try {
      const status = await readCodexAccount();
      setAccount(accountState(status));
      if (status.connected) {
        setLogin(undefined);
      } else {
        setLoginError('OpenAI sign in has not completed yet.');
      }
    } catch (cause) {
      setLoginError(errorMessage(cause, 'Unable to refresh the Codex account.'));
    } finally {
      setBusy(false);
    }
  }

  async function cancelLogin() {
    const pendingLogin = login;
    setLogin(undefined);
    setLoginError(undefined);
    if (!pendingLogin) return;
    try {
      await cancelCodexLogin(pendingLogin.loginID);
    } catch {
      setAccount({ kind: 'error', message: 'The pending Codex login could not be cancelled.' });
    }
  }

  async function retryAccount() {
    setAccount({ kind: 'loading' });
    try {
      setAccount(accountState(await readCodexAccount()));
    } catch (cause) {
      setAccount({
        kind: 'error',
        message: errorMessage(cause, 'Unable to read the Codex account.'),
      });
    }
  }

  async function disconnectAccount() {
    setBusy(true);
    try {
      await logoutCodexAccount();
      setAccount({ kind: 'disconnected' });
      setDisconnectOpen(false);
    } catch (cause) {
      setAccount({
        kind: 'error',
        message: errorMessage(cause, 'Unable to disconnect the Codex account.'),
      });
      setDisconnectOpen(false);
    } finally {
      setBusy(false);
    }
  }

  const connected = account.kind === 'connected';
  const title = connected ? account.status.email || 'ChatGPT account connected' : 'Codex account';

  return (
    <>
      <Card className="h-full" variant="default">
        <Card.Header>
          <div className="flex w-full items-start justify-between gap-4">
            <div className="flex items-center gap-3">
              {connected ? (
                <UserRoundCheck aria-hidden="true" size={20} />
              ) : (
                <CloudOff aria-hidden="true" size={20} />
              )}
              <div>
                <Card.Title>{title}</Card.Title>
                <Card.Description>{accountDescription(account)}</Card.Description>
              </div>
            </div>
            {account.kind === 'loading' ? (
              <Spinner size="sm" />
            ) : connected ? (
              <Chip color="success" size="sm" variant="soft">
                {formatCodexPlanName(account.status.planType) || 'Connected'}
              </Chip>
            ) : account.kind === 'error' ? (
              <Chip color="danger" size="sm" variant="soft">
                Unavailable
              </Chip>
            ) : (
              <Chip color="warning" size="sm" variant="soft">
                Not connected
              </Chip>
            )}
          </div>
        </Card.Header>
        <Card.Content>
          <Typography color="muted" type="body-sm">
            {connected
              ? 'This account supplies models for project API requests.'
              : 'Connect a ChatGPT account before sending project API requests.'}
          </Typography>
        </Card.Content>
        <Card.Footer className="mt-auto">
          {account.kind === 'disconnected' ? (
            <Button isDisabled={busy} size="sm" variant="secondary" onPress={startLogin}>
              <span className="inline-flex items-center gap-2">
                {busy ? <Spinner color="current" size="sm" /> : null}
                <span>Connect account</span>
              </span>
            </Button>
          ) : account.kind === 'error' ? (
            <Button size="sm" variant="secondary" onPress={retryAccount}>
              <span className="inline-flex items-center gap-2">
                <RefreshCw aria-hidden="true" size={14} />
                <span>Retry</span>
              </span>
            </Button>
          ) : connected && allowDisconnect ? (
            <Button
              aria-label="Disconnect Codex account"
              isDisabled={busy}
              size="sm"
              variant="danger-soft"
              onPress={() => setDisconnectOpen(true)}
            >
              <LogOut aria-hidden="true" size={14} />
              Disconnect
            </Button>
          ) : null}
        </Card.Footer>
      </Card>

      <Modal.Backdrop
        isOpen={login !== undefined}
        onOpenChange={(isOpen) => {
          if (!isOpen) void cancelLogin();
        }}
      >
        <Modal.Container placement="center" size="sm">
          <Modal.Dialog>
            <Modal.Header>
              <Modal.Icon>
                <ExternalLink aria-hidden="true" size={20} />
              </Modal.Icon>
              <Modal.Heading>Connect ChatGPT</Modal.Heading>
            </Modal.Header>
            <Modal.Body>
              <div className="flex flex-col gap-4">
                <Typography color="muted" type="body-sm">
                  Open the OpenAI device page, sign in, and enter this one-time code.
                </Typography>
                <Surface variant="secondary">
                  <div className="flex flex-col gap-1 p-4">
                    <Typography color="muted" type="body-xs">
                      One-time code
                    </Typography>
                    <Typography type="code" weight="semibold">
                      {login?.userCode}
                    </Typography>
                  </div>
                </Surface>
                {loginError ? (
                  <Alert status="warning">
                    <Alert.Indicator />
                    <Alert.Content>
                      <Alert.Title>Still waiting</Alert.Title>
                      <Alert.Description>{loginError}</Alert.Description>
                    </Alert.Content>
                  </Alert>
                ) : null}
                {login ? (
                  <Link href={login.verificationURL} rel="noopener noreferrer" target="_blank">
                    Open OpenAI sign in
                    <Link.Icon />
                  </Link>
                ) : null}
              </div>
            </Modal.Body>
            <Modal.Footer>
              <Button isDisabled={busy} variant="secondary" onPress={cancelLogin}>
                Cancel login
              </Button>
              <Button isDisabled={busy} onPress={checkConnection}>
                <span className="inline-flex items-center gap-2">
                  {busy ? <Spinner color="current" size="sm" /> : null}
                  <span>Check connection</span>
                </span>
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>

      <AlertDialog.Backdrop isOpen={disconnectOpen} onOpenChange={setDisconnectOpen}>
        <AlertDialog.Container placement="center" size="sm">
          <AlertDialog.Dialog>
            <AlertDialog.Header>
              <AlertDialog.Icon status="danger">
                <ShieldAlert aria-hidden="true" size={20} />
              </AlertDialog.Icon>
              <AlertDialog.Heading>Disconnect Codex?</AlertDialog.Heading>
            </AlertDialog.Header>
            <AlertDialog.Body>
              Disconnects the local Codex session. Project API requests will stop until you connect
              again.
            </AlertDialog.Body>
            <AlertDialog.Footer>
              <Button variant="secondary" onPress={() => setDisconnectOpen(false)}>
                Cancel
              </Button>
              <Button isDisabled={busy} variant="danger" onPress={() => void disconnectAccount()}>
                {busy ? <Spinner color="current" size="sm" /> : null}
                Disconnect account
              </Button>
            </AlertDialog.Footer>
          </AlertDialog.Dialog>
        </AlertDialog.Container>
      </AlertDialog.Backdrop>
    </>
  );
}

const knownPlanNames: Readonly<Record<string, string>> = {
  free: 'Free',
  go: 'Go',
  plus: 'Plus',
  pro: 'Pro 20x',
  pro20x: 'Pro 20x',
  pro5x: 'Pro 5x',
  prolite: 'Pro 5x',
};

export function formatCodexPlanName(planType?: string): string {
  if (!planType) return '';
  const normalized = planType
    .trim()
    .toLowerCase()
    .replaceAll(/[^a-z0-9]/g, '');
  const known = knownPlanNames[normalized];
  if (known) return known;
  return planType
    .replaceAll(/[^a-zA-Z0-9]+/g, ' ')
    .trim()
    .slice(0, 48)
    .split(/\s+/)
    .filter(Boolean)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1).toLowerCase())
    .join(' ');
}

function accountState(status: CodexAccountStatus): AccountState {
  return status.connected ? { kind: 'connected', status } : { kind: 'disconnected' };
}

function accountDescription(account: AccountState): string {
  switch (account.kind) {
    case 'connected':
      return `Authenticated through Codex${account.status.type ? ` using ${account.status.type}` : ''}.`;
    case 'disconnected':
      return 'Authentication has not been configured yet.';
    case 'error':
      return account.message;
    case 'loading':
      return 'Checking Codex authentication…';
  }
}

function errorMessage(cause: unknown, fallback: string): string {
  return cause instanceof Error ? cause.message : fallback;
}
