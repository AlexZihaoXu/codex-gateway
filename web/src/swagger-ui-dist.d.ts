declare module 'swagger-ui-dist/swagger-ui-bundle.js' {
  interface SwaggerUIOptions {
    deepLinking?: boolean;
    defaultModelsExpandDepth?: number;
    displayRequestDuration?: boolean;
    docExpansion?: 'full' | 'list' | 'none';
    domNode: HTMLElement;
    filter?: boolean | string;
    persistAuthorization?: boolean;
    url: string;
    validatorUrl?: string | null;
  }

  type SwaggerUIBundle = (options: SwaggerUIOptions) => unknown;

  const SwaggerUIBundle: SwaggerUIBundle;

  export default SwaggerUIBundle;
}
