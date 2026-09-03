import { Card } from '@heroui/react';
import { useMemo } from 'react';
import {
  Bar,
  BarChart,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
  Tooltip as ChartTooltip,
  XAxis,
  YAxis,
} from 'recharts';

import type { ModelUsage } from './project-api';

export default function ModelUsageChart({ models }: { models: ModelUsage[] }) {
  const data = useMemo(
    () =>
      models.map((model) => ({
        cachedInput: model.cachedInputTokens,
        model: model.model,
        reasoning: model.reasoningOutputTokens,
        uncachedInput: model.inputTokens - model.cachedInputTokens,
        visibleOutput: model.outputTokens - model.reasoningOutputTokens,
      })),
    [models],
  );
  return (
    <Card>
      <Card.Header>
        <Card.Title>Tokens by model</Card.Title>
        <Card.Description>
          Non-overlapping token categories. Each stacked bar equals that model&apos;s total tokens.
        </Card.Description>
      </Card.Header>
      <Card.Content>
        <figure aria-label="Tokens by model" className="h-full min-h-64 w-full">
          <ResponsiveContainer height={Math.max(240, data.length * 64)} width="100%">
            <BarChart accessibilityLayer data={data} layout="vertical" margin={{ left: 12 }}>
              <CartesianGrid horizontal={false} stroke="currentColor" strokeOpacity={0.1} />
              <XAxis tickFormatter={formatCompactNumber} tickLine={false} type="number" />
              <YAxis dataKey="model" tickLine={false} type="category" width={128} />
              <ChartTooltip formatter={(value) => formatNumber(Number(value))} />
              <Legend />
              <Bar dataKey="uncachedInput" fill="#2563eb" name="Uncached input" stackId="tokens" />
              <Bar dataKey="cachedInput" fill="#06b6d4" name="Cached input" stackId="tokens" />
              <Bar dataKey="visibleOutput" fill="#8b5cf6" name="Visible output" stackId="tokens" />
              <Bar
                dataKey="reasoning"
                fill="#f59e0b"
                name="Reasoning output"
                radius={[0, 4, 4, 0]}
                stackId="tokens"
              />
            </BarChart>
          </ResponsiveContainer>
        </figure>
      </Card.Content>
    </Card>
  );
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value);
}

function formatCompactNumber(value: number): string {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 1, notation: 'compact' }).format(
    value,
  );
}
