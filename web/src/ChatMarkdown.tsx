import { Button, Tooltip } from '@heroui/react';
import { Check, Clipboard, Code2, PanelRightOpen } from 'lucide-react';
import {
  isValidElement,
  memo,
  type ReactNode,
  useCallback,
  useEffect,
  useRef,
  useState,
} from 'react';
import ReactMarkdown, { type Options } from 'react-markdown';
import rehypeHighlight from 'rehype-highlight';
import rehypeMathjax from 'rehype-mathjax/svg';
import remarkGfm from 'remark-gfm';
import remarkMath from 'remark-math';

import { rehypeStreamWords } from './rehypeStreamWords';

type RehypePlugins = NonNullable<Options['rehypePlugins']>;

const staticRehypePlugins: RehypePlugins = [rehypeMathjax, rehypeHighlight];
const streamingRehypePlugins: RehypePlugins = [rehypeMathjax, rehypeHighlight, rehypeStreamWords];
const remarkPlugins: NonNullable<Options['remarkPlugins']> = [remarkGfm, remarkMath];

const languageNames: Readonly<Record<string, string>> = {
  bash: 'Bash',
  cpp: 'C++',
  csharp: 'C#',
  css: 'CSS',
  html: 'HTML',
  javascript: 'JavaScript',
  js: 'JavaScript',
  json: 'JSON',
  jsx: 'JSX',
  latex: 'LaTeX',
  py: 'Python',
  python: 'Python',
  sh: 'Shell',
  sql: 'SQL',
  tex: 'TeX',
  ts: 'TypeScript',
  tsx: 'TSX',
  typescript: 'TypeScript',
};

export interface ReasoningSummaryParts {
  body: string;
  title: string | undefined;
}

export function splitReasoningSummary(summary: string): ReasoningSummaryParts {
  const match = /^\s*\*\*([^*\n]{1,120})\*\*\s*(?:\r?\n){2,}([\s\S]*)$/u.exec(summary);
  const title = match?.[1]?.trim();
  const body = match?.[2]?.trim();
  if (!title || body === undefined || title.endsWith(':')) {
    return { body: summary, title: undefined };
  }
  return { body, title };
}

export function deferIncompleteTable(markdown: string): string {
  if ((markdown.match(/```/gu)?.length ?? 0) % 2 === 1) return markdown;

  const lines = markdown.split('\n');
  let firstTableLine = lines.length;
  while (firstTableLine > 0 && /^\s*\|/u.test(lines[firstTableLine - 1] ?? '')) {
    firstTableLine -= 1;
  }
  if (firstTableLine === lines.length) return markdown;

  const tableLines = lines.slice(firstTableLine);
  const headerCells = tableCells(tableLines[0] ?? '');
  const delimiterCells = tableCells(tableLines[1] ?? '');
  const hasCompleteDelimiter =
    headerCells.length > 0 &&
    delimiterCells.length === headerCells.length &&
    delimiterCells.every((cell) => /^:?-{3,}:?$/u.test(cell));
  return hasCompleteDelimiter ? markdown : lines.slice(0, firstTableLine).join('\n');
}

function tableCells(line: string): string[] {
  const trimmed = line.trim().replace(/^\|/u, '').replace(/\|$/u, '');
  return trimmed ? trimmed.split('|').map((cell) => cell.trim()) : [];
}

function normalizeDisplayMath(markdown: string): string {
  return markdown
    .split(/(```[\s\S]*?```|~~~[\s\S]*?~~~|`[^`\n]*`)/gu)
    .map((segment, index) =>
      index % 2 === 1
        ? segment
        : segment.replace(
            /\$\$([\s\S]+?)\$\$/gu,
            (_match, expression: string) => `\n\n$$\n${expression.trim()}\n$$\n\n`,
          ),
    )
    .join('');
}

function nestedText(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(nestedText).join('');
  if (isValidElement<{ children?: ReactNode }>(node)) return nestedText(node.props.children);
  return '';
}

function codeLanguage(node: ReactNode): string | undefined {
  if (!isValidElement<{ className?: unknown }>(node)) return undefined;
  const { className } = node.props;
  if (typeof className !== 'string') return undefined;
  return /(?:^|\s)language-(\S+)/u.exec(className)?.[1];
}

function languageLabel(language: string | undefined): string {
  if (!language) return 'Code';
  return languageNames[language] ?? `${language.charAt(0).toUpperCase()}${language.slice(1)}`;
}

function isPreviewableHtml(language: string | undefined, code: string): boolean {
  if (language === 'html' || language === 'htm') return true;
  if (language !== undefined) return false;
  return /^\s*(?:<!doctype\s+html\b|<html\b)/iu.test(code);
}

function CopyCodeButton({ code }: { code: string }) {
  const [copied, setCopied] = useState(false);
  const resetTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(
    () => () => {
      if (resetTimer.current !== undefined) clearTimeout(resetTimer.current);
    },
    [],
  );
  const copy = useCallback(() => {
    if (!navigator.clipboard) return;
    void navigator.clipboard.writeText(code).then(() => {
      setCopied(true);
      if (resetTimer.current !== undefined) clearTimeout(resetTimer.current);
      resetTimer.current = setTimeout(() => setCopied(false), 1600);
    });
  }, [code]);

  return (
    <Tooltip>
      <Button
        aria-label={copied ? 'Code copied' : 'Copy code'}
        isIconOnly
        size="sm"
        variant="ghost"
        onPress={copy}
      >
        {copied ? (
          <Check aria-hidden="true" size={15} />
        ) : (
          <Clipboard aria-hidden="true" size={15} />
        )}
      </Button>
      <Tooltip.Content>{copied ? 'Copied' : 'Copy code'}</Tooltip.Content>
    </Tooltip>
  );
}

export const ChatMarkdown = memo(function ChatMarkdown({
  content,
  onOpenHtmlPreview,
  streaming = false,
}: {
  content: string;
  onOpenHtmlPreview?: ((html: string) => void) | undefined;
  streaming?: boolean;
}) {
  const markdown = normalizeDisplayMath(streaming ? deferIncompleteTable(content) : content);
  return (
    <div className="chat-markdown">
      <ReactMarkdown
        components={{
          a: ({ children, href }) => (
            <a href={href} rel="noreferrer noopener" target="_blank">
              {children}
            </a>
          ),
          pre: ({ children }) => {
            const language = codeLanguage(children);
            const code = nestedText(children);
            const canPreview =
              !streaming && onOpenHtmlPreview !== undefined && isPreviewableHtml(language, code);
            return (
              <div className="chat-code-block">
                <div className="chat-code-header">
                  <span className="flex items-center gap-2 text-xs font-medium">
                    <Code2 aria-hidden="true" size={15} />
                    {languageLabel(language)}
                  </span>
                  <div className="flex items-center gap-1">
                    {canPreview ? (
                      <Button
                        aria-label="Open HTML preview"
                        size="sm"
                        variant="ghost"
                        onPress={() => onOpenHtmlPreview(code)}
                      >
                        <PanelRightOpen aria-hidden="true" size={15} />
                        Preview
                      </Button>
                    ) : null}
                    <CopyCodeButton code={code} />
                  </div>
                </div>
                <pre>{children}</pre>
              </div>
            );
          },
          table: ({ children }) => (
            <div className="chat-table-scroll">
              <table>{children}</table>
            </div>
          ),
        }}
        rehypePlugins={streaming ? streamingRehypePlugins : staticRehypePlugins}
        remarkPlugins={remarkPlugins}
      >
        {markdown}
      </ReactMarkdown>
      {streaming && content ? <span aria-hidden="true" className="chat-stream-caret" /> : null}
    </div>
  );
});
