export async function readJSONResponse(response: Response): Promise<unknown> {
  const body: unknown = await response.json().catch(() => undefined);
  if (!response.ok) {
    if (isRecord(body) && typeof body.detail === 'string') {
      throw new Error(body.detail);
    }
    throw new Error(`The gateway request failed with status ${response.status}.`);
  }
  return body;
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}
