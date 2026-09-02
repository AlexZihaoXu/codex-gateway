import {
  Alert,
  AlertDialog,
  Button,
  Chip,
  Disclosure,
  Dropdown,
  Form,
  Input,
  Label,
  ListBox,
  Modal,
  ScrollShadow,
  Select,
  Spinner,
  Surface,
  TextArea,
  TextField,
  Tooltip,
  Typography,
} from '@heroui/react';
import {
  Activity,
  ArrowDown,
  Brain,
  EllipsisVertical,
  MessageSquare,
  Pencil,
  Plus,
  Send,
  ShieldAlert,
  SlidersHorizontal,
  Square,
  Trash2,
} from 'lucide-react';
import {
  type FormEvent,
  type KeyboardEvent,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import {
  deleteChatSession,
  listChatSessions,
  listPlaygroundModels,
  type PersistedChatSession,
  type PlaygroundEndpoint,
  type PlaygroundMessage,
  type PlaygroundUsage,
  type ReasoningEffort,
  renameChatSession,
  streamPlayground,
} from './chat-api';
import { ChatMarkdown, splitReasoningSummary } from './ChatMarkdown';
import { HtmlPreview } from './HtmlPreview';
import { listProjects, type Project } from './project-api';

interface DisplayMessage extends PlaygroundMessage {
  fresh?: boolean | undefined;
  id: string;
  reasoning?: string | undefined;
  streaming?: boolean | undefined;
  usage?: PlaygroundUsage | undefined;
}

interface ChatSession {
  endpoint: PlaygroundEndpoint;
  id: string;
  instructions: string;
  messages: DisplayMessage[];
  model: string;
  projectID: string;
  reasoningEffort: ReasoningEffort;
  title: string;
}

interface TurnOptions {
  previousMessages?: DisplayMessage[];
  restartSession?: boolean;
  session?: ChatSession;
}

const reasoningEfforts: readonly ReasoningEffort[] = [
  'none',
  'low',
  'medium',
  'high',
  'xhigh',
  'max',
  'ultra',
];

const dashboardChatProject = 'dashboard-chat';
const nearConversationBottom = 120;

const samplePrompts = [
  {
    label: 'Explain an API response',
    prompt: 'Explain this API response and identify any OpenAI compatibility issues.',
  },
  {
    label: 'Debug a stream',
    prompt: 'Help me debug an SSE stream that stops before reporting terminal usage.',
  },
  {
    label: 'Draft a test plan',
    prompt: 'Draft a focused test plan for an OpenAI-compatible chat endpoint.',
  },
] as const;

export function ChatView() {
  const [models, setModels] = useState<string[]>();
  const [model, setModel] = useState('');
  const [projects, setProjects] = useState<Project[]>();
  const [projectID, setProjectID] = useState('');
  const [endpoint, setEndpoint] = useState<PlaygroundEndpoint>('chat-completions');
  const [reasoningEffort, setReasoningEffort] = useState<ReasoningEffort>('medium');
  const [instructions, setInstructions] = useState('');
  const [draft, setDraft] = useState('');
  const [sessions, setSessions] = useState<ChatSession[]>([]);
  const [activeSessionID, setActiveSessionID] = useState<string>();
  const [error, setError] = useState<string>();
  const [loadingSetup, setLoadingSetup] = useState(true);
  const [setupFailed, setSetupFailed] = useState(false);
  const [streaming, setStreaming] = useState(false);
  const [followLatest, setFollowLatest] = useState(true);
  const [unseenActivity, setUnseenActivity] = useState(false);
  const [editingMessageID, setEditingMessageID] = useState<string>();
  const [editDraft, setEditDraft] = useState('');
  const [renameCandidate, setRenameCandidate] = useState<ChatSession>();
  const [renameDraft, setRenameDraft] = useState('');
  const [deleteCandidate, setDeleteCandidate] = useState<ChatSession>();
  const [mutatingSessionID, setMutatingSessionID] = useState<string>();
  const [previewHtml, setPreviewHtml] = useState<string>();
  const activeRequest = useRef<AbortController | undefined>(undefined);
  const composer = useRef<HTMLTextAreaElement | null>(null);
  const conversation = useRef<HTMLDivElement | null>(null);
  const followLatestRef = useRef(true);
  const smoothScrolling = useRef(false);

  const activeSession = useMemo(
    () => sessions.find((session) => session.id === activeSessionID),
    [activeSessionID, sessions],
  );
  const conversationUsage = useMemo(
    () => aggregateConversationUsage(activeSession?.messages ?? []),
    [activeSession?.messages],
  );

  const loadSetup = useCallback((signal?: AbortSignal) => {
    void Promise.all([listPlaygroundModels(signal), listProjects(signal), listChatSessions(signal)])
      .then(([availableModels, availableProjects, persistedSessions]) => {
        if (signal?.aborted) return;
        const restoredSessions = persistedSessions.map(displaySession);
        const restoredSession = restoredSessions[0];
        setModels(availableModels);
        setModel(
          restoredSession && availableModels.includes(restoredSession.model)
            ? restoredSession.model
            : (availableModels[0] ?? ''),
        );
        setProjects(availableProjects);
        const restoredProjectID = restoredSession?.projectID ?? '';
        setProjectID(
          restoredProjectID === dashboardChatProject ||
            availableProjects.some((project) => project.id === restoredProjectID)
            ? restoredProjectID
            : (availableProjects[0]?.id ?? dashboardChatProject),
        );
        setSessions(restoredSessions);
        setActiveSessionID(restoredSession?.id);
        setEndpoint(restoredSession?.endpoint ?? 'chat-completions');
        setReasoningEffort(restoredSession?.reasoningEffort ?? 'medium');
        setInstructions(restoredSession?.instructions ?? '');
        if (availableModels.length === 0) {
          setSetupFailed(true);
          setError('Codex did not report any available models.');
        }
      })
      .catch((cause: unknown) => {
        if (signal?.aborted) return;
        setModels(undefined);
        setModel('');
        setProjects(undefined);
        setProjectID('');
        setSetupFailed(true);
        setError(errorMessage(cause, 'Unable to prepare dashboard chat.'));
      })
      .finally(() => {
        if (!signal?.aborted) setLoadingSetup(false);
      });
  }, []);

  function retrySetup() {
    setLoadingSetup(true);
    setSetupFailed(false);
    setError(undefined);
    void loadSetup();
  }

  useEffect(() => {
    const controller = new AbortController();
    void loadSetup(controller.signal);
    return () => controller.abort();
  }, [loadSetup]);

  const setFollowingLatest = useCallback((next: boolean) => {
    followLatestRef.current = next;
    setFollowLatest(next);
    if (next) setUnseenActivity(false);
  }, []);

  useLayoutEffect(() => {
    if (!followLatestRef.current || smoothScrolling.current || !conversation.current) return;
    conversation.current.scrollTop = conversation.current.scrollHeight;
  }, [activeSession?.messages]);

  function updateAssistant(sessionID: string, messageID: string, update: Partial<DisplayMessage>) {
    setSessions((current) =>
      current.map((session) =>
        session.id === sessionID
          ? {
              ...session,
              messages: session.messages.map((message) =>
                message.id === messageID ? { ...message, ...update } : message,
              ),
            }
          : session,
      ),
    );
  }

  function appendAssistant(
    sessionID: string,
    messageID: string,
    contentDelta: string,
    reasoningDelta: string,
  ) {
    setSessions((current) =>
      current.map((session) =>
        session.id === sessionID
          ? {
              ...session,
              messages: session.messages.map((message) =>
                message.id === messageID
                  ? {
                      ...message,
                      content: message.content + contentDelta,
                      reasoning: (message.reasoning ?? '') + reasoningDelta,
                    }
                  : message,
              ),
            }
          : session,
      ),
    );
  }

  async function runTurn(content: string, options: TurnOptions = {}): Promise<boolean> {
    const sourceSession = options.session ?? activeSession;
    if (!content || !model || !projectID || streaming) return false;

    setError(undefined);
    setStreaming(true);
    const controller = new AbortController();
    activeRequest.current = controller;
    const isNewSession = sourceSession === undefined;
    const sessionID = sourceSession?.id ?? `chat-${globalThis.crypto.randomUUID()}`;
    const userMessage: DisplayMessage = {
      content,
      fresh: true,
      id: nextDisplayMessageID(),
      role: 'user',
    };
    const assistantMessage: DisplayMessage = {
      content: '',
      fresh: true,
      id: nextDisplayMessageID(),
      reasoning: '',
      role: 'assistant',
      streaming: true,
    };
    const requestProjectID = sourceSession?.projectID ?? projectID;
    const previousMessages = options.previousMessages ?? sourceSession?.messages ?? [];
    const rollbackMessages = sourceSession?.messages ?? [];
    const requestMessages: PlaygroundMessage[] = [
      ...(instructions.trim() ? [{ content: instructions.trim(), role: 'system' as const }] : []),
      ...[...previousMessages, userMessage].map(({ content: text, role }) => ({
        content: text,
        role,
      })),
    ];
    const session: ChatSession = {
      endpoint,
      id: sessionID,
      instructions,
      messages: [...previousMessages, userMessage, assistantMessage],
      model,
      projectID: requestProjectID,
      reasoningEffort,
      title: sourceSession?.title ?? chatTitle(content),
    };
    setSessions((current) => {
      const exists = current.some((item) => item.id === sessionID);
      return exists
        ? current.map((item) => (item.id === sessionID ? session : item))
        : [session, ...current];
    });
    setActiveSessionID(sessionID);
    smoothScrolling.current = false;
    setFollowingLatest(true);

    let pendingText = '';
    let pendingReasoning = '';
    let renderFrame: number | undefined;
    const scheduleRender = () => {
      if (renderFrame !== undefined) return;
      renderFrame = requestAnimationFrame(() => {
        renderFrame = undefined;
        const textDelta = pendingText;
        const reasoningDelta = pendingReasoning;
        pendingText = '';
        pendingReasoning = '';
        if (!followLatestRef.current) setUnseenActivity(true);
        appendAssistant(sessionID, assistantMessage.id, textDelta, reasoningDelta);
      });
    };
    try {
      const result = await streamPlayground(
        {
          endpoint,
          messages: requestMessages,
          model,
          ...(requestProjectID === dashboardChatProject ? {} : { projectID: requestProjectID }),
          reasoningEffort,
          ...(options.restartSession ? { restartSession: true } : {}),
          sessionID,
          signal: controller.signal,
          title: session.title,
        },
        (delta) => {
          pendingText += delta;
          scheduleRender();
        },
        (delta) => {
          pendingReasoning += delta;
          scheduleRender();
        },
      );
      if (renderFrame !== undefined) cancelAnimationFrame(renderFrame);
      renderFrame = undefined;
      updateAssistant(sessionID, assistantMessage.id, {
        content: result.text,
        reasoning: result.reasoning,
        streaming: false,
        usage: result.usage,
      });
      if (!followLatestRef.current) setUnseenActivity(true);
      return true;
    } catch (cause) {
      if (renderFrame !== undefined) cancelAnimationFrame(renderFrame);
      renderFrame = undefined;
      if (!controller.signal.aborted) {
        setError(errorMessage(cause, 'Unable to stream the response.'));
      }
      if (isNewSession) {
        setSessions((current) => current.filter((item) => item.id !== sessionID));
        setActiveSessionID(undefined);
      } else {
        setSessions((current) =>
          current.map((item) =>
            item.id === sessionID ? { ...item, messages: rollbackMessages } : item,
          ),
        );
      }
      return false;
    } finally {
      if (renderFrame !== undefined) cancelAnimationFrame(renderFrame);
      if (activeRequest.current === controller) activeRequest.current = undefined;
      setStreaming(false);
    }
  }

  function sendMessage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const content = draft.trim();
    if (!content || streaming) return;
    setDraft('');
    composer.current?.focus();
    void runTurn(content).then((sent) => {
      if (!sent) setDraft((current) => current || content);
    });
  }

  function startEditing(message: DisplayMessage) {
    setEditingMessageID(message.id);
    setEditDraft(message.content);
  }

  function cancelEditing() {
    setEditingMessageID(undefined);
    setEditDraft('');
  }

  function saveEditedMessage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const content = editDraft.trim();
    const session = activeSession;
    if (!content || !session || !editingMessageID || streaming) return;
    const editedIndex = session.messages.findIndex((message) => message.id === editingMessageID);
    if (editedIndex < 0) return;
    const previousMessages = session.messages.slice(0, editedIndex);
    cancelEditing();
    setPreviewHtml(undefined);
    void runTurn(content, { previousMessages, restartSession: true, session });
  }

  function handleComposerKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === 'Escape' && streaming) {
      event.preventDefault();
      activeRequest.current?.abort();
      return;
    }
    if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) return;
    event.preventDefault();
    event.currentTarget.form?.requestSubmit();
  }

  function handleConversationScroll() {
    const element = conversation.current;
    if (!element) return;
    const distanceFromBottom = element.scrollHeight - element.scrollTop - element.clientHeight;
    if (distanceFromBottom <= nearConversationBottom) smoothScrolling.current = false;
    setFollowingLatest(distanceFromBottom <= nearConversationBottom);
  }

  function jumpToLatest() {
    const element = conversation.current;
    if (!element) return;
    smoothScrolling.current = true;
    if (typeof element.scrollTo === 'function') {
      const reduceMotion = globalThis.matchMedia?.('(prefers-reduced-motion: reduce)').matches;
      element.scrollTo({ behavior: reduceMotion ? 'auto' : 'smooth', top: element.scrollHeight });
    } else {
      element.scrollTop = element.scrollHeight;
      smoothScrolling.current = false;
    }
    setFollowingLatest(true);
  }

  function startNewChat() {
    activeRequest.current?.abort();
    setActiveSessionID(undefined);
    setDraft('');
    setError(undefined);
    setInstructions('');
    setPreviewHtml(undefined);
    cancelEditing();
    smoothScrolling.current = false;
    setFollowingLatest(true);
    requestAnimationFrame(() => composer.current?.focus());
  }

  function openChat(sessionID: string) {
    const session = sessions.find((item) => item.id === sessionID);
    if (!session) return;
    setActiveSessionID(session.id);
    setEndpoint(session.endpoint);
    setInstructions(session.instructions);
    setReasoningEffort(session.reasoningEffort);
    if (models?.includes(session.model)) setModel(session.model);
    if (
      session.projectID === dashboardChatProject ||
      projects?.some((project) => project.id === session.projectID)
    ) {
      setProjectID(session.projectID);
    }
    setError(undefined);
    setPreviewHtml(undefined);
    cancelEditing();
    smoothScrolling.current = false;
    setFollowingLatest(true);
    requestAnimationFrame(() => composer.current?.focus());
  }

  function beginRename(session: ChatSession) {
    setRenameCandidate(session);
    setRenameDraft(session.title);
  }

  async function saveRename(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const candidate = renameCandidate;
    const title = renameDraft.trim();
    if (!candidate || !title || mutatingSessionID) return;
    setMutatingSessionID(candidate.id);
    setError(undefined);
    try {
      await renameChatSession(candidate.id, title);
      setSessions((current) =>
        current.map((session) => (session.id === candidate.id ? { ...session, title } : session)),
      );
      setRenameCandidate(undefined);
    } catch (cause) {
      setError(errorMessage(cause, 'Unable to rename the chat.'));
    } finally {
      setMutatingSessionID(undefined);
    }
  }

  async function removeChat(candidate: ChatSession) {
    if (mutatingSessionID) return;
    setMutatingSessionID(candidate.id);
    setError(undefined);
    try {
      await deleteChatSession(candidate.id);
      setSessions((current) => current.filter((session) => session.id !== candidate.id));
      if (activeSessionID === candidate.id) {
        setActiveSessionID(undefined);
        setInstructions('');
        setDraft('');
        setPreviewHtml(undefined);
      }
      setDeleteCandidate(undefined);
    } catch (cause) {
      setError(errorMessage(cause, 'Unable to delete the chat.'));
    } finally {
      setMutatingSessionID(undefined);
    }
  }

  return (
    <section
      aria-labelledby="chat-heading"
      className="flex w-full min-w-0 flex-col gap-6 py-8 lg:h-[calc(100dvh-73px)] lg:min-h-[640px]"
    >
      <div className="flex flex-col gap-1">
        <Typography id="chat-heading" type="h2">
          Chat
        </Typography>
        <Typography color="muted">
          Exercise the compatibility endpoints without leaving the gateway.
        </Typography>
      </div>

      {error ? (
        <Alert status="danger">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Title>Playground request failed</Alert.Title>
            <Alert.Description>{error}</Alert.Description>
            {setupFailed ? (
              <Button size="sm" variant="secondary" onPress={retrySetup}>
                Retry setup
              </Button>
            ) : null}
          </Alert.Content>
        </Alert>
      ) : null}

      <div
        className={`grid min-h-[620px] gap-4 lg:min-h-0 lg:flex-1 lg:grid-cols-[260px_minmax(0,1fr)] ${previewHtml ? 'xl:grid-cols-[240px_minmax(480px,1.15fr)_minmax(360px,0.85fr)]' : ''}`}
      >
        <Surface className="h-64 min-h-0 rounded-2xl lg:h-auto" variant="secondary">
          <aside aria-label="Chat history" className="flex h-full min-h-0 flex-col gap-4 p-4">
            <div className="flex items-center justify-between gap-3">
              <Typography type="h5">Chats</Typography>
              <Tooltip delay={400}>
                <Button
                  isIconOnly
                  aria-label="New chat"
                  isDisabled={streaming}
                  size="sm"
                  variant="ghost"
                  onPress={startNewChat}
                >
                  <Plus aria-hidden="true" size={16} />
                </Button>
                <Tooltip.Content placement="right">New chat</Tooltip.Content>
              </Tooltip>
            </div>
            <ScrollShadow className="min-h-0 flex-1" size={24}>
              {sessions.length === 0 ? (
                <div className="flex flex-col gap-1 py-2">
                  <MessageSquare aria-hidden="true" className="text-muted" size={18} />
                  <Typography color="muted" type="body-sm">
                    No chats yet
                  </Typography>
                  <Typography color="muted" type="body-xs">
                    Completed chats are saved on this gateway.
                  </Typography>
                </div>
              ) : (
                <div aria-label="Chats" className="flex flex-col gap-1" role="list">
                  {sessions.map((session) => (
                    <Surface
                      className="flex min-w-0 items-center gap-1 rounded-xl p-1"
                      key={session.id}
                      role="listitem"
                      variant={activeSessionID === session.id ? 'default' : 'transparent'}
                    >
                      <Button
                        aria-label={`Open chat ${session.title}`}
                        className="min-w-0 flex-1 justify-start"
                        variant="ghost"
                        onPress={() => openChat(session.id)}
                      >
                        <MessageSquare aria-hidden="true" className="shrink-0" size={16} />
                        <span className="flex min-w-0 flex-col items-start text-left">
                          <span className="w-full truncate">{session.title}</span>
                          <span className="w-full truncate text-xs text-muted">
                            {session.model}
                          </span>
                        </span>
                      </Button>
                      <Dropdown>
                        <Button
                          aria-label={`Actions for ${session.title}`}
                          isDisabled={streaming || mutatingSessionID === session.id}
                          size="sm"
                          slot="trigger"
                          variant="ghost"
                        >
                          {mutatingSessionID === session.id ? (
                            <Spinner color="current" size="sm" />
                          ) : (
                            <EllipsisVertical aria-hidden="true" size={16} />
                          )}
                        </Button>
                        <Dropdown.Popover>
                          <Dropdown.Menu
                            aria-label={`Actions for ${session.title}`}
                            onAction={(key) => {
                              if (String(key) === 'rename') beginRename(session);
                              if (String(key) === 'delete') setDeleteCandidate(session);
                            }}
                          >
                            <Dropdown.Item id="rename" textValue="Rename">
                              <Pencil aria-hidden="true" size={15} />
                              Rename
                            </Dropdown.Item>
                            <Dropdown.Item id="delete" textValue="Delete">
                              <Trash2 aria-hidden="true" size={15} />
                              Delete
                            </Dropdown.Item>
                          </Dropdown.Menu>
                        </Dropdown.Popover>
                      </Dropdown>
                    </Surface>
                  ))}
                </div>
              )}
            </ScrollShadow>
          </aside>
        </Surface>

        <Surface className="min-h-[620px] overflow-hidden rounded-2xl lg:min-h-0">
          <section aria-label="Chat workspace" className="flex h-full min-h-0 flex-col p-4 sm:p-6">
            <div
              className={`flex min-h-0 flex-1 flex-col ${activeSession?.messages.length ? '' : 'justify-center py-10'}`}
            >
              {activeSession && activeSession.messages.length > 0 ? (
                <div className="relative flex min-h-0 flex-1 flex-col">
                  <div className="mx-auto flex w-full max-w-3xl flex-wrap items-center justify-between gap-2 border-b border-default-200 pb-3">
                    <Typography className="truncate" type="h5">
                      {activeSession.title}
                    </Typography>
                    <div className="flex shrink-0 items-center gap-2" aria-live="polite">
                      {streaming && activeSession.messages.some((message) => message.streaming) ? (
                        <Chip size="sm" variant="soft">
                          <Spinner color="current" size="sm" />
                          <Chip.Label>Generating response</Chip.Label>
                        </Chip>
                      ) : null}
                      <Typography color="muted" type="body-xs">
                        {formatConversationSummary(conversationUsage)}
                      </Typography>
                    </div>
                  </div>
                  <ScrollShadow
                    ref={conversation}
                    aria-label="Conversation"
                    aria-busy={streaming}
                    aria-live="polite"
                    aria-relevant="additions text"
                    className="mx-auto min-h-0 w-full max-w-3xl flex-1 overscroll-contain pb-6 pt-6"
                    role="log"
                    tabIndex={-1}
                    onScroll={handleConversationScroll}
                  >
                    <div className="flex flex-col gap-5">
                      {activeSession.messages.map((message) => (
                        <ConversationMessage
                          editing={editingMessageID === message.id}
                          editingDisabled={streaming}
                          editValue={editDraft}
                          key={message.id}
                          message={message}
                          onCancelEdit={cancelEditing}
                          onEditValueChange={setEditDraft}
                          onSaveEdit={saveEditedMessage}
                          onStartEdit={() => startEditing(message)}
                          onOpenHtmlPreview={setPreviewHtml}
                        />
                      ))}
                    </div>
                  </ScrollShadow>
                  {!followLatest ? (
                    <Button
                      aria-label="Jump to latest message"
                      className="chat-jump-enter absolute bottom-3 left-1/2 -translate-x-1/2"
                      size="sm"
                      variant="secondary"
                      onPress={jumpToLatest}
                    >
                      <ArrowDown aria-hidden="true" size={15} />
                      {unseenActivity ? 'New response' : 'Latest'}
                    </Button>
                  ) : null}
                </div>
              ) : (
                <div className="flex flex-col items-center gap-5 text-center">
                  <div className="flex flex-col gap-2">
                    <Typography type="h2">Ready to test the gateway?</Typography>
                    <Typography color="muted">
                      Choose a sample prompt or write your own message.
                    </Typography>
                  </div>
                  <div aria-label="Sample prompts" className="flex flex-wrap justify-center gap-2">
                    {samplePrompts.map((sample) => (
                      <Button
                        key={sample.label}
                        size="sm"
                        variant="secondary"
                        onPress={() => {
                          setDraft(sample.prompt);
                          composer.current?.focus();
                        }}
                      >
                        {sample.label}
                      </Button>
                    ))}
                  </div>
                </div>
              )}

              <Form
                className={`mx-auto w-full max-w-3xl shrink-0 ${activeSession?.messages.length ? 'mt-auto' : 'mt-8'}`}
                onSubmit={sendMessage}
              >
                <Surface className="rounded-2xl" variant="secondary">
                  <div className="flex flex-col gap-3 p-3">
                    <div className="flex flex-wrap items-center gap-2">
                      <Chip size="sm" variant="soft">
                        <Activity aria-hidden="true" size={13} />
                        <Chip.Label>Dashboard tracked</Chip.Label>
                      </Chip>
                      <Typography color="muted" type="body-xs">
                        {projectID === dashboardChatProject
                          ? 'Usage is counted in the Dashboard Chat bucket.'
                          : 'Usage and limits apply to the selected project.'}
                      </Typography>
                    </div>
                    <Disclosure>
                      <Disclosure.Heading>
                        <Button slot="trigger" size="sm" variant="ghost">
                          <SlidersHorizontal aria-hidden="true" size={15} />
                          Instructions
                          <Disclosure.Indicator />
                        </Button>
                      </Disclosure.Heading>
                      <Disclosure.Content>
                        <Disclosure.Body className="pt-2">
                          <TextField fullWidth value={instructions} onChange={setInstructions}>
                            <Label className="sr-only">Instructions</Label>
                            <Input
                              placeholder="Add optional system instructions"
                              variant="secondary"
                            />
                          </TextField>
                        </Disclosure.Body>
                      </Disclosure.Content>
                    </Disclosure>
                    <TextField fullWidth value={draft} onChange={setDraft}>
                      <Label className="sr-only">Message</Label>
                      <TextArea
                        ref={composer}
                        className="max-h-40 field-sizing-content"
                        placeholder="Message Codex Gateway"
                        rows={2}
                        variant="secondary"
                        onKeyDown={handleComposerKeyDown}
                      />
                    </TextField>
                    <div className="flex flex-wrap items-center gap-2">
                      <Select
                        className="min-w-40"
                        isDisabled={loadingSetup || Boolean(activeSession)}
                        selectedKey={projectID || null}
                        onSelectionChange={(key) => setProjectID(String(key))}
                      >
                        <Label className="sr-only">Usage project</Label>
                        <Select.Trigger>
                          <Select.Value />
                          <Select.Indicator />
                        </Select.Trigger>
                        <Select.Popover>
                          <ListBox>
                            <ListBox.Item id={dashboardChatProject} textValue="Dashboard Chat">
                              Dashboard Chat
                              <ListBox.ItemIndicator />
                            </ListBox.Item>
                            {(projects ?? []).map((project) => (
                              <ListBox.Item
                                id={project.id}
                                key={project.id}
                                textValue={project.name}
                              >
                                {project.name}
                                <ListBox.ItemIndicator />
                              </ListBox.Item>
                            ))}
                          </ListBox>
                        </Select.Popover>
                      </Select>
                      <Select
                        className="min-w-48 flex-1"
                        isDisabled={loadingSetup || !models?.length}
                        selectedKey={model || null}
                        onSelectionChange={(key) => setModel(String(key))}
                      >
                        <Label className="sr-only">Model</Label>
                        <Select.Trigger>
                          <Select.Value />
                          <Select.Indicator />
                        </Select.Trigger>
                        <Select.Popover>
                          <ListBox>
                            {(models ?? []).map((item) => (
                              <ListBox.Item id={item} key={item} textValue={item}>
                                {item}
                                <ListBox.ItemIndicator />
                              </ListBox.Item>
                            ))}
                          </ListBox>
                        </Select.Popover>
                      </Select>
                      <Select
                        className="min-w-40"
                        selectedKey={endpoint}
                        onSelectionChange={(key) => {
                          const value = String(key);
                          if (value === 'chat-completions' || value === 'responses')
                            setEndpoint(value);
                        }}
                      >
                        <Label className="sr-only">Endpoint</Label>
                        <Select.Trigger>
                          <Select.Value />
                          <Select.Indicator />
                        </Select.Trigger>
                        <Select.Popover>
                          <ListBox>
                            <ListBox.Item id="chat-completions" textValue="Chat Completions">
                              Chat Completions
                              <ListBox.ItemIndicator />
                            </ListBox.Item>
                            <ListBox.Item id="responses" textValue="Responses">
                              Responses
                              <ListBox.ItemIndicator />
                            </ListBox.Item>
                          </ListBox>
                        </Select.Popover>
                      </Select>
                      <Select
                        className="min-w-32"
                        selectedKey={reasoningEffort}
                        onSelectionChange={(key) => {
                          const value = String(key);
                          if (isReasoningEffort(value)) setReasoningEffort(value);
                        }}
                      >
                        <Label className="sr-only">Reasoning</Label>
                        <Select.Trigger>
                          <Select.Value />
                          <Select.Indicator />
                        </Select.Trigger>
                        <Select.Popover>
                          <ListBox>
                            {reasoningEfforts.map((effort) => (
                              <ListBox.Item id={effort} key={effort} textValue={effort}>
                                {capitalize(effort)}
                                <ListBox.ItemIndicator />
                              </ListBox.Item>
                            ))}
                          </ListBox>
                        </Select.Popover>
                      </Select>
                      <Tooltip delay={400}>
                        {streaming ? (
                          <Button
                            isIconOnly
                            aria-label="Stop response"
                            className="chat-control-enter"
                            size="lg"
                            variant="secondary"
                            onPress={() => activeRequest.current?.abort()}
                          >
                            <Square aria-hidden="true" size={16} />
                          </Button>
                        ) : (
                          <Button
                            isIconOnly
                            aria-label="Send message"
                            className="chat-control-enter"
                            isDisabled={
                              loadingSetup ||
                              !projectID ||
                              !models?.length ||
                              !model ||
                              draft.trim().length === 0
                            }
                            size="lg"
                            type="submit"
                          >
                            <Send aria-hidden="true" size={16} />
                          </Button>
                        )}
                        <Tooltip.Content placement="top">
                          {streaming ? 'Stop generating (Esc)' : 'Send message (Enter)'}
                        </Tooltip.Content>
                      </Tooltip>
                    </div>
                  </div>
                </Surface>
              </Form>
            </div>
          </section>
        </Surface>

        {previewHtml ? (
          <HtmlPreview html={previewHtml} onClose={() => setPreviewHtml(undefined)} />
        ) : null}
      </div>

      <Modal.Backdrop
        isOpen={renameCandidate !== undefined}
        onOpenChange={(open) => {
          if (!open) setRenameCandidate(undefined);
        }}
      >
        <Modal.Container placement="center" size="sm">
          <Modal.Dialog>
            <Form onSubmit={saveRename}>
              <Modal.Header>
                <Modal.Heading>Rename chat</Modal.Heading>
              </Modal.Header>
              <Modal.Body>
                <TextField fullWidth isRequired value={renameDraft} onChange={setRenameDraft}>
                  <Label>Chat title</Label>
                  <Input autoFocus maxLength={200} />
                </TextField>
              </Modal.Body>
              <Modal.Footer>
                <Button variant="secondary" onPress={() => setRenameCandidate(undefined)}>
                  Cancel
                </Button>
                <Button
                  isDisabled={Boolean(mutatingSessionID) || renameDraft.trim().length === 0}
                  type="submit"
                >
                  {mutatingSessionID ? <Spinner color="current" size="sm" /> : null}
                  Save
                </Button>
              </Modal.Footer>
            </Form>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>

      <AlertDialog.Backdrop
        isOpen={deleteCandidate !== undefined}
        onOpenChange={(open) => {
          if (!open) setDeleteCandidate(undefined);
        }}
      >
        <AlertDialog.Container placement="center" size="sm">
          <AlertDialog.Dialog>
            <AlertDialog.Header>
              <AlertDialog.Icon status="danger">
                <ShieldAlert aria-hidden="true" size={20} />
              </AlertDialog.Icon>
              <AlertDialog.Heading>Delete {deleteCandidate?.title}?</AlertDialog.Heading>
            </AlertDialog.Header>
            <AlertDialog.Body>
              This permanently removes the conversation and its messages from this gateway.
            </AlertDialog.Body>
            <AlertDialog.Footer>
              <Button variant="secondary" onPress={() => setDeleteCandidate(undefined)}>
                Cancel
              </Button>
              <Button
                isDisabled={Boolean(mutatingSessionID)}
                variant="danger"
                onPress={() => {
                  if (deleteCandidate) void removeChat(deleteCandidate);
                }}
              >
                {mutatingSessionID ? <Spinner color="current" size="sm" /> : null}
                Delete chat
              </Button>
            </AlertDialog.Footer>
          </AlertDialog.Dialog>
        </AlertDialog.Container>
      </AlertDialog.Backdrop>
    </section>
  );
}

interface ConversationMessageProps {
  editing: boolean;
  editingDisabled: boolean;
  editValue: string;
  message: DisplayMessage;
  onCancelEdit: () => void;
  onEditValueChange: (value: string) => void;
  onSaveEdit: (event: FormEvent<HTMLFormElement>) => void;
  onStartEdit: () => void;
  onOpenHtmlPreview: (html: string) => void;
}

function ConversationMessage({
  editing,
  editingDisabled,
  editValue,
  message,
  onCancelEdit,
  onEditValueChange,
  onSaveEdit,
  onStartEdit,
  onOpenHtmlPreview,
}: ConversationMessageProps) {
  if (message.role === 'user') {
    return (
      <div
        className={`group flex justify-end gap-1 ${message.fresh ? 'chat-turn-enter' : 'chat-message-block'}`}
      >
        {editing ? (
          <Form className="w-full max-w-[85%]" onSubmit={onSaveEdit}>
            <Surface className="rounded-2xl p-3" variant="secondary">
              <div className="flex flex-col gap-3">
                <TextField fullWidth value={editValue} onChange={onEditValueChange}>
                  <Label className="sr-only">Edit message</Label>
                  <TextArea autoFocus rows={3} variant="secondary" />
                </TextField>
                <div className="flex justify-end gap-2">
                  <Button size="sm" variant="ghost" onPress={onCancelEdit}>
                    Cancel
                  </Button>
                  <Button isDisabled={!editValue.trim()} size="sm" type="submit">
                    Save and regenerate
                  </Button>
                </div>
              </div>
            </Surface>
          </Form>
        ) : (
          <>
            <Tooltip delay={400}>
              <Button
                isIconOnly
                aria-label="Edit message"
                className="self-center opacity-100 transition-opacity sm:opacity-0 sm:group-focus-within:opacity-100 sm:group-hover:opacity-100"
                isDisabled={editingDisabled}
                size="sm"
                variant="ghost"
                onPress={onStartEdit}
              >
                <Pencil aria-hidden="true" size={15} />
              </Button>
              <Tooltip.Content placement="left">Edit message</Tooltip.Content>
            </Tooltip>
            <Surface className="max-w-[85%] rounded-2xl" variant="secondary">
              <div className="whitespace-pre-wrap px-4 py-3">{message.content}</div>
            </Surface>
          </>
        )}
      </div>
    );
  }

  return (
    <article
      className={`flex max-w-full flex-col gap-3 px-1 py-2 ${message.fresh ? 'chat-turn-enter' : 'chat-message-block'}`}
    >
      <ReasoningBlock
        answerStarted={Boolean(message.content)}
        reasoning={message.reasoning}
        streaming={Boolean(message.streaming)}
      />
      <AssistantContent message={message} onOpenHtmlPreview={onOpenHtmlPreview} />
      {message.usage ? (
        <Typography color="muted" type="body-xs">
          {formatTurnUsageSummary(message.usage)}
        </Typography>
      ) : null}
    </article>
  );
}

function ReasoningBlock({
  answerStarted,
  reasoning,
  streaming,
}: {
  answerStarted: boolean;
  reasoning?: string | undefined;
  streaming: boolean;
}) {
  const summary = splitReasoningSummary(reasoning ?? '');
  if (streaming && !answerStarted) {
    return (
      <div aria-live="polite" className="flex max-h-36 flex-col gap-2 py-1">
        <div className="flex items-center gap-2">
          <Spinner aria-label={summary.title ?? 'Thinking'} size="sm" />
          <Typography type="body-sm">{summary.title ?? 'Thinking'}</Typography>
        </div>
        {summary.body ? (
          <ScrollShadow className="min-h-0 pl-6 text-sm text-muted">
            <ChatMarkdown content={summary.body} streaming />
          </ScrollShadow>
        ) : null}
      </div>
    );
  }
  if (!reasoning) return null;
  if (!summary.body) {
    return (
      <div className="flex items-center gap-2 py-1">
        <Brain aria-hidden="true" size={15} />
        <Typography type="body-sm">{summary.title ?? reasoning}</Typography>
      </div>
    );
  }
  return (
    <Disclosure>
      <Disclosure.Heading>
        <Button
          aria-label={summary.title ? `Reasoning summary: ${summary.title}` : 'Reasoning summary'}
          slot="trigger"
          size="sm"
          variant="ghost"
        >
          <Brain aria-hidden="true" size={15} />
          {summary.title ?? 'Reasoning summary'}
          <Disclosure.Indicator />
        </Button>
      </Disclosure.Heading>
      <Disclosure.Content>
        <Disclosure.Body className="pl-2 pt-2">
          <ChatMarkdown content={summary.body} streaming={streaming} />
        </Disclosure.Body>
      </Disclosure.Content>
    </Disclosure>
  );
}

function AssistantContent({
  message,
  onOpenHtmlPreview,
}: {
  message: DisplayMessage;
  onOpenHtmlPreview: (html: string) => void;
}) {
  if (!message.content) return null;
  return (
    <ChatMarkdown
      content={message.content}
      streaming={Boolean(message.streaming)}
      onOpenHtmlPreview={onOpenHtmlPreview}
    />
  );
}

function displaySession(session: PersistedChatSession): ChatSession {
  return {
    endpoint: session.endpoint,
    id: session.id,
    instructions: session.instructions,
    messages: session.messages.map((message) => ({
      ...message,
      id: nextDisplayMessageID(),
    })),
    model: session.model,
    projectID: session.projectID || dashboardChatProject,
    reasoningEffort: session.reasoningEffort,
    title: session.title,
  };
}

function nextDisplayMessageID(): string {
  return `message-${globalThis.crypto.randomUUID()}`;
}

function chatTitle(content: string): string {
  return content.length > 48 ? `${content.slice(0, 47)}…` : content;
}

function capitalize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

function isReasoningEffort(value: string): value is ReasoningEffort {
  return reasoningEfforts.some((effort) => effort === value);
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value);
}

function formatTurnUsageSummary(usage: PlaygroundUsage): string {
  const uncachedInputTokens = Math.max(usage.inputTokens - usage.cachedInputTokens, 0);
  return `Turn · ${formatNumber(usage.totalTokens)} total · ${formatNumber(usage.inputTokens)} input (${formatNumber(usage.cachedInputTokens)} cached, ${formatNumber(uncachedInputTokens)} uncached) · ${formatNumber(usage.outputTokens)} output (${formatNumber(usage.reasoningOutputTokens)} reasoning)`;
}

function aggregateConversationUsage(
  messages: DisplayMessage[],
): PlaygroundUsage & { turns: number } {
  return messages.reduce<PlaygroundUsage & { turns: number }>(
    (total, message) => {
      if (!message.usage) return total;
      return {
        cachedInputTokens: total.cachedInputTokens + message.usage.cachedInputTokens,
        inputTokens: total.inputTokens + message.usage.inputTokens,
        outputTokens: total.outputTokens + message.usage.outputTokens,
        reasoningOutputTokens: total.reasoningOutputTokens + message.usage.reasoningOutputTokens,
        totalTokens: total.totalTokens + message.usage.totalTokens,
        turns: total.turns + 1,
      };
    },
    {
      cachedInputTokens: 0,
      inputTokens: 0,
      outputTokens: 0,
      reasoningOutputTokens: 0,
      totalTokens: 0,
      turns: 0,
    },
  );
}

function formatConversationSummary(usage: PlaygroundUsage & { turns: number }): string {
  const turnLabel = usage.turns === 1 ? 'turn' : 'turns';
  return `Conversation · ${formatNumber(usage.totalTokens)} tokens · ${formatNumber(usage.turns)} ${turnLabel} · ${formatNumber(usage.cachedInputTokens)} cached`;
}

function errorMessage(cause: unknown, fallback: string): string {
  return cause instanceof Error ? cause.message : fallback;
}
