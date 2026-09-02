import { Button, Modal, Surface, Tooltip, Typography } from '@heroui/react';
import { Maximize2, X } from 'lucide-react';
import { useMemo, useState } from 'react';

const previewPolicy = [
  "default-src 'none'",
  "script-src 'unsafe-inline'",
  "style-src 'unsafe-inline'",
  'img-src data: blob:',
  'media-src data: blob:',
  'font-src data:',
  "connect-src 'none'",
  "object-src 'none'",
  "frame-src 'none'",
  "form-action 'none'",
  "base-uri 'none'",
].join('; ');

const previewHead = `<meta http-equiv="Content-Security-Policy" content="${previewPolicy}"><meta name="referrer" content="no-referrer"><meta name="viewport" content="width=device-width, initial-scale=1"><style>html,body{min-height:100%}body{margin:0;background:Canvas;color:CanvasText}</style>`;

export function secureHtmlPreview(html: string): string {
  if (/<head(?:\s[^>]*)?>/iu.test(html)) {
    return html.replace(/<head(?:\s[^>]*)?>/iu, (head) => `${head}${previewHead}`);
  }
  if (/<html(?:\s[^>]*)?>/iu.test(html)) {
    return html.replace(/<html(?:\s[^>]*)?>/iu, (root) => `${root}<head>${previewHead}</head>`);
  }
  return `<!doctype html><html><head>${previewHead}</head><body>${html}</body></html>`;
}

function PreviewFrame({ document }: { document: string }) {
  return (
    <iframe
      className="h-full w-full border-0 bg-background"
      referrerPolicy="no-referrer"
      sandbox="allow-scripts"
      srcDoc={document}
      title="HTML preview"
    />
  );
}

export function HtmlPreview({ html, onClose }: { html: string; onClose: () => void }) {
  const [maximized, setMaximized] = useState(false);
  const document = useMemo(() => secureHtmlPreview(html), [html]);

  return (
    <>
      <Surface
        className="min-h-[520px] overflow-hidden rounded-2xl lg:col-span-2 xl:col-span-1 xl:min-h-0"
        variant="secondary"
      >
        <section aria-label="HTML preview" className="flex h-full min-h-0 flex-col">
          <header className="flex min-h-14 items-center justify-between gap-3 border-b border-border px-4">
            <div className="min-w-0">
              <Typography className="truncate" type="h5">
                HTML preview
              </Typography>
              <Typography color="muted" type="body-xs">
                Sandboxed, local document
              </Typography>
            </div>
            <div className="flex items-center gap-1">
              <Tooltip>
                <Button
                  aria-label="Maximize HTML preview"
                  isIconOnly
                  size="sm"
                  variant="ghost"
                  onPress={() => setMaximized(true)}
                >
                  <Maximize2 aria-hidden="true" size={16} />
                </Button>
                <Tooltip.Content>Maximize</Tooltip.Content>
              </Tooltip>
              <Tooltip>
                <Button
                  aria-label="Close HTML preview"
                  isIconOnly
                  size="sm"
                  variant="ghost"
                  onPress={onClose}
                >
                  <X aria-hidden="true" size={16} />
                </Button>
                <Tooltip.Content>Close preview</Tooltip.Content>
              </Tooltip>
            </div>
          </header>
          <div className="min-h-[460px] flex-1 bg-background xl:min-h-0">
            <PreviewFrame document={document} />
          </div>
        </section>
      </Surface>

      <Modal.Backdrop isOpen={maximized} onOpenChange={setMaximized}>
        <Modal.Container placement="center" size="full">
          <Modal.Dialog>
            <Modal.CloseTrigger />
            <Modal.Header>
              <Modal.Heading>HTML preview</Modal.Heading>
            </Modal.Header>
            <Modal.Body>
              <div className="h-[calc(100dvh-8rem)] min-h-[420px] overflow-hidden rounded-xl border border-border bg-background">
                <PreviewFrame document={document} />
              </div>
            </Modal.Body>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </>
  );
}
