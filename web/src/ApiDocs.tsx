import { useEffect, useRef } from 'react';
import SwaggerUIBundle from 'swagger-ui-dist/swagger-ui-bundle.js';

import 'swagger-ui-dist/swagger-ui.css';
import './api-docs.css';

export default function ApiDocs() {
  const swaggerRoot = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const root = swaggerRoot.current;
    const initialization = window.setTimeout(() => {
      if (!root) {
        return;
      }

      SwaggerUIBundle({
        deepLinking: true,
        defaultModelsExpandDepth: 1,
        displayRequestDuration: true,
        docExpansion: 'list',
        domNode: root,
        filter: true,
        persistAuthorization: false,
        url: '/openapi.json',
        validatorUrl: null,
      });
    }, 0);

    return () => {
      window.clearTimeout(initialization);
      root?.replaceChildren();
    };
  }, []);

  return (
    <main className="api-docs-page" aria-label="Codex Gateway API reference">
      <div ref={swaggerRoot} />
    </main>
  );
}
