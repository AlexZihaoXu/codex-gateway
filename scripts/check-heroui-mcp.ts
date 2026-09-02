import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StdioClientTransport } from '@modelcontextprotocol/sdk/client/stdio.js';

const requiredTools = [
  'get_component_docs',
  'get_component_source_code',
  'get_component_source_styles',
  'get_docs',
  'get_theme_variables',
  'list_components',
] as const;

const requestedComponents = process.argv.slice(2);
const client = new Client({ name: 'codex-gateway-heroui-check', version: '0.1.0' });
const transport = new StdioClientTransport({
  args: ['node_modules/@heroui/react-mcp/dist/stdio.js'],
  command: process.execPath,
});

try {
  await client.connect(transport);

  const tools = await client.listTools();
  const availableTools = new Set(tools.tools.map((tool) => tool.name));
  const missingTools = requiredTools.filter((tool) => !availableTools.has(tool));

  if (missingTools.length > 0) {
    throw new Error(`HeroUI MCP is missing tools: ${missingTools.join(', ')}`);
  }

  const componentList = await client.callTool({ name: 'list_components' });
  if (componentList.isError) {
    throw new Error('HeroUI MCP could not list components.');
  }

  console.log(`HeroUI MCP exposes ${tools.tools.length} expected documentation tools.`);
  if (requestedComponents.length === 0) {
    for (const item of componentList.content) {
      if (item.type === 'text') {
        console.log(item.text);
      }
    }
  }

  if (requestedComponents.length > 0) {
    const componentDocs = await client.callTool({
      arguments: { components: requestedComponents },
      name: 'get_component_docs',
    });

    if (componentDocs.isError) {
      throw new Error(`HeroUI MCP could not load: ${requestedComponents.join(', ')}`);
    }

    for (const item of componentDocs.content) {
      if (item.type === 'text') {
        console.log(item.text);
      }
    }
  }
} finally {
  await client.close();
}
