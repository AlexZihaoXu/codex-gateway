import type { Element, ElementContent, Root, Text } from 'hast';

const skippedElements = new Set([
  'code',
  'math',
  'mjx-container',
  'pre',
  'script',
  'style',
  'svg',
  'table',
]);

function wrapText(text: Text): ElementContent[] {
  return text.value.split(/(\s+)/u).flatMap((part): ElementContent[] => {
    if (!part) return [];
    if (/^\s+$/u.test(part)) return [{ type: 'text', value: part }];
    return [
      {
        children: [{ type: 'text', value: part }],
        properties: { className: ['chat-stream-token'] },
        tagName: 'span',
        type: 'element',
      },
    ];
  });
}

function isGeneratedToken(element: Element): boolean {
  const classNames = element.properties?.className;
  return (
    element.tagName === 'span' &&
    Array.isArray(classNames) &&
    classNames.includes('chat-stream-token')
  );
}

function containsRenderedMath(element: Element): boolean {
  const classNames = element.properties?.className;
  return (
    element.tagName === 'mjx-container' ||
    element.tagName === 'svg' ||
    (Array.isArray(classNames) && classNames.includes('math'))
  );
}

function wrapDescendantText(element: Element): void {
  const children: ElementContent[] = [];
  for (const child of element.children) {
    if (child.type === 'text') {
      children.push(...wrapText(child));
      continue;
    }
    if (
      child.type === 'element' &&
      !skippedElements.has(child.tagName) &&
      !isGeneratedToken(child) &&
      !containsRenderedMath(child)
    ) {
      wrapDescendantText(child);
    }
    children.push(child);
  }
  element.children = children;
}

export function rehypeStreamWords() {
  return (tree: Root): void => {
    const activeBlock = [...tree.children]
      .reverse()
      .find((child): child is Element => child.type === 'element');
    if (!activeBlock || skippedElements.has(activeBlock.tagName)) return;
    wrapDescendantText(activeBlock);
  };
}
