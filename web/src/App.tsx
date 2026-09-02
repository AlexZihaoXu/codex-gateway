import {
  Button,
  Card,
  Chip,
  Link,
  ProgressBar,
  Separator,
  Skeleton,
  Surface,
  Tabs,
  Typography,
} from '@heroui/react';
import { lazy, Suspense, useEffect, useState } from 'react';
import {
  Activity,
  BookOpen,
  Boxes,
  CircleGauge,
  KeyRound,
  LockKeyhole,
  LogOut,
  MessageSquare,
  Radio,
  Settings,
} from 'lucide-react';

import { AuthenticationGate } from './AuthenticationGate';
import { CodexAccountCard } from './CodexAccountCard';
import { APIKeysView, ProjectsView, SettingsView, UsageView } from './DashboardViews';

const ApiDocs = lazy(() => import('./ApiDocs'));
const ChatView = lazy(() => import('./ChatView').then((module) => ({ default: module.ChatView })));

const navigation = [
  { id: 'chat', name: 'Chat', icon: MessageSquare, path: '/chat' },
  { id: 'overview', name: 'Overview', icon: CircleGauge, path: '/overview' },
  { id: 'projects', name: 'Projects', icon: Boxes, path: '/projects' },
  { id: 'api-keys', name: 'API keys', icon: KeyRound, path: '/api-keys' },
  { id: 'usage', name: 'Usage', icon: Activity, path: '/usage' },
  { id: 'settings', name: 'Settings', icon: Settings, path: '/settings' },
] as const;

type TabID = (typeof navigation)[number]['id'];

export function App() {
  if (window.location.pathname === '/doc') {
    return (
      <Suspense
        fallback={
          <main className="docs-loading">
            <p>Codex Gateway</p>
            <h1>API reference</h1>
            <span>Loading OpenAPI documentation…</span>
          </main>
        }
      >
        <ApiDocs />
      </Suspense>
    );
  }

  return <AuthenticationGate>{(signOut) => <Dashboard onSignOut={signOut} />}</AuthenticationGate>;
}

interface DashboardProps {
  onSignOut: () => Promise<void>;
}

function Dashboard({ onSignOut }: DashboardProps) {
  const [selectedTab, setSelectedTab] = useState<TabID>(() =>
    tabIDFromPath(window.location.pathname),
  );

  useEffect(() => {
    function followHistory() {
      setSelectedTab(tabIDFromPath(window.location.pathname));
    }
    window.addEventListener('popstate', followHistory);
    return () => window.removeEventListener('popstate', followHistory);
  }, []);

  function selectTab(tab: TabID) {
    const path = navigation.find((item) => item.id === tab)?.path;
    if (path && window.location.pathname !== path) window.history.pushState({}, '', path);
    setSelectedTab(tab);
  }

  return (
    <div className="min-h-screen bg-background text-foreground">
      <Tabs
        selectedKey={selectedTab}
        onSelectionChange={(key) => {
          const requested = String(key);
          if (isTabID(requested)) selectTab(requested);
        }}
      >
        <header className="border-b border-border bg-surface">
          <div className="mx-auto grid max-w-[1440px] grid-cols-[1fr_auto] items-center gap-x-4 gap-y-3 px-5 py-4 sm:px-8 lg:grid-cols-[auto_minmax(0,1fr)_auto] lg:px-12">
            <Link aria-label="Codex Gateway home" href="/overview">
              <span className="inline-flex items-center gap-2">
                <Radio aria-hidden="true" size={20} strokeWidth={2.25} />
                <span className="font-semibold">Codex Gateway</span>
              </span>
            </Link>
            <nav
              aria-label="Gateway sections"
              className="col-span-2 row-start-2 min-w-0 justify-self-stretch lg:col-span-1 lg:col-start-2 lg:row-start-1 lg:justify-self-center"
            >
              <Tabs.ListContainer>
                <Tabs.List aria-label="Gateway navigation">
                  {navigation.map(({ icon: Icon, id, name }) => (
                    <Tabs.Tab id={id} key={id}>
                      <span className="inline-flex items-center gap-2 whitespace-nowrap">
                        <Icon aria-hidden="true" size={16} />
                        <span>{name}</span>
                      </span>
                      <Tabs.Indicator />
                    </Tabs.Tab>
                  ))}
                </Tabs.List>
              </Tabs.ListContainer>
            </nav>
            <div className="col-start-2 row-start-1 flex items-center gap-2 lg:col-start-3">
              <Chip color="success" size="sm" variant="soft">
                <LockKeyhole aria-hidden="true" size={13} />
                <Chip.Label>Loopback only</Chip.Label>
              </Chip>
              <Button aria-label="Sign out" size="sm" variant="ghost" onPress={onSignOut}>
                <LogOut aria-hidden="true" size={16} />
              </Button>
            </div>
          </div>
        </header>
        <DashboardPanels selectedTab={selectedTab} />
      </Tabs>
    </div>
  );
}

function DashboardPanels({ selectedTab }: { selectedTab: TabID }) {
  return (
    <main
      className={`mx-auto min-h-[calc(100vh-73px)] w-full min-w-0 px-5 sm:px-8 lg:px-12 ${selectedTab === 'chat' ? 'max-w-[1800px]' : 'max-w-[1440px]'}`}
    >
      <Tabs.Panel className="w-full min-w-0" id="overview">
        {selectedTab === 'overview' ? <OverviewView /> : null}
      </Tabs.Panel>
      <Tabs.Panel className="w-full min-w-0" id="projects">
        {selectedTab === 'projects' ? <ProjectsView /> : null}
      </Tabs.Panel>
      <Tabs.Panel className="w-full min-w-0" id="api-keys">
        {selectedTab === 'api-keys' ? <APIKeysView /> : null}
      </Tabs.Panel>
      <Tabs.Panel className="w-full min-w-0" id="usage">
        {selectedTab === 'usage' ? <UsageView /> : null}
      </Tabs.Panel>
      <Tabs.Panel className="w-full min-w-0" id="chat">
        {selectedTab === 'chat' ? (
          <Suspense fallback={<ChatLoading />}>
            <ChatView />
          </Suspense>
        ) : null}
      </Tabs.Panel>
      <Tabs.Panel className="w-full min-w-0" id="settings">
        {selectedTab === 'settings' ? <SettingsView /> : null}
      </Tabs.Panel>
    </main>
  );
}

function ChatLoading() {
  return (
    <div aria-label="Loading chat" className="flex w-full flex-col gap-6 py-8" role="status">
      <div className="flex flex-col gap-2">
        <Skeleton className="h-8 w-24 rounded-lg" />
        <Skeleton className="h-4 w-80 max-w-full rounded-lg" />
      </div>
      <div className="grid min-h-[620px] gap-4 lg:grid-cols-[260px_minmax(0,1fr)]">
        <Skeleton className="h-64 rounded-2xl lg:h-full" />
        <Skeleton className="min-h-[620px] rounded-2xl" />
      </div>
    </div>
  );
}

function OverviewView() {
  return (
    <div className="flex flex-col gap-8 py-8 lg:py-10">
      <header className="flex flex-col gap-5 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex max-w-2xl flex-col gap-2">
          <Typography type="h1">Gateway overview</Typography>
          <Typography color="muted">
            Local status, account readiness, and project usage in one place.
          </Typography>
        </div>
        <Link href="/doc">
          <span className="inline-flex items-center gap-2">
            <BookOpen aria-hidden="true" size={16} />
            <span>API reference</span>
            <Link.Icon />
          </span>
        </Link>
      </header>

      <Separator variant="tertiary" />

      <section aria-labelledby="status-heading">
        <div className="mb-4 flex items-center justify-between gap-4">
          <Typography id="status-heading" type="h4">
            System status
          </Typography>
          <Typography color="muted" type="body-xs">
            v0.1.0
          </Typography>
        </div>
        <div className="grid gap-4 xl:grid-cols-2">
          <Card>
            <Card.Header>
              <div className="flex w-full items-start justify-between gap-4">
                <div className="flex items-center gap-3">
                  <Activity aria-hidden="true" size={20} />
                  <div>
                    <Card.Title>Gateway online</Card.Title>
                    <Card.Description>
                      Go runtime and embedded dashboard are healthy.
                    </Card.Description>
                  </div>
                </div>
                <Chip color="success" size="sm" variant="soft">
                  Healthy
                </Chip>
              </div>
            </Card.Header>
            <Card.Footer>
              <Typography color="muted" type="body-xs">
                Listening on 127.0.0.1:11435
              </Typography>
            </Card.Footer>
          </Card>
          <CodexAccountCard />
        </div>
      </section>

      <section aria-labelledby="allowance-heading">
        <div className="mb-4 flex flex-col gap-1">
          <Typography id="allowance-heading" type="h4">
            Usage at a glance
          </Typography>
          <Typography color="muted" type="body-sm">
            Detailed project totals are available from the Usage tab.
          </Typography>
        </div>
        <Surface variant="secondary">
          <div className="grid gap-8 p-6 md:grid-cols-[minmax(0,1.4fr)_minmax(220px,0.6fr)] md:p-8">
            <div className="flex flex-col gap-5">
              <div>
                <Typography type="h5">Project usage limits</Typography>
                <Typography color="muted" type="body-sm">
                  Combine minute, hourly, daily, weekly, or monthly windows.
                </Typography>
              </div>
              <ProgressBar aria-label="Project usage limits" color="accent" value={0}>
                <ProgressBar.Track>
                  <ProgressBar.Fill />
                </ProgressBar.Track>
              </ProgressBar>
            </div>
            <div className="flex items-center md:justify-end">
              <Typography color="muted" type="body-sm">
                Limits are enforced before each request.
              </Typography>
            </div>
          </div>
        </Surface>
      </section>
    </div>
  );
}

function isTabID(value: string): value is TabID {
  return navigation.some((item) => item.id === value);
}

function tabIDFromPath(path: string): TabID {
  if (path === '/') return 'overview';
  return navigation.find((item) => item.path === path)?.id ?? 'overview';
}
