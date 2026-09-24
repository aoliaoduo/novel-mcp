#!/usr/bin/env node

import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

function arg(name, fallback = "") {
  const i = process.argv.indexOf(name);
  return i >= 0 && i + 1 < process.argv.length ? process.argv[i + 1] : fallback;
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

const here = path.dirname(fileURLToPath(import.meta.url));
const sdkRoot = process.env.MCP_TS_SDK_ROOT ||
  path.resolve(here, "../.local/mcp-ts-sdk/node_modules/@modelcontextprotocol/client/dist");
const endpoint = arg("--url");
const bearer = arg("--bearer");
const project = arg("--project", "ts-sdk-smoke");

if (!endpoint) {
  console.error("usage: node scripts/mcp-ts-smoke.mjs --url <mcp-url> [--bearer <token>] [--project <id>]");
  process.exit(2);
}

let Client;
let StreamableHTTPClientTransport;
try {
  ({ Client, StreamableHTTPClientTransport } = await import(pathToFileURL(path.join(sdkRoot, "index.mjs")).href));
} catch (error) {
  console.error("TypeScript MCP SDK 未安装。先执行：");
  console.error("  npm install --prefix .local/mcp-ts-sdk --save-exact @modelcontextprotocol/client@2.0.0 @modelcontextprotocol/core@2.0.0");
  throw error;
}

const headers = bearer ? { Authorization: `Bearer ${bearer}` } : {};
const client = new Client(
  { name: "novel-mcp-ts-smoke", version: "1.0.0" },
  { capabilities: {}, versionNegotiation: { mode: { pin: "2026-07-28" } } },
);
const transport = new StreamableHTTPClientTransport(new URL(endpoint), {
  requestInit: { headers },
});

let createdRevision = "";
let latestRevision = "";
try {
  await client.connect(transport);

  const tools = await client.listTools();
  assert(tools.tools.length === 25, `tools=${tools.tools.length}, want 25`);
  for (const tool of tools.tools) {
    assert(tool.inputSchema && typeof tool.inputSchema === "object", `${tool.name} 缺 inputSchema`);
    assert(tool.outputSchema && typeof tool.outputSchema === "object", `${tool.name} 缺 outputSchema`);
  }

  const prompts = await client.listPrompts();
  assert(prompts.prompts.length === 5, `prompts=${prompts.prompts.length}, want 5`);
  const writerPrompt = await client.getPrompt({ name: "novel_writer" });
  assert(writerPrompt.messages?.length === 1, "novel_writer prompt 消息数不对");

  const resources = await client.listResources();
  assert(resources.resources.length === 1 && resources.resources[0].uri === "novel://projects",
    "固定 resources 列表不对");
  const templates = await client.listResourceTemplates();
  assert(templates.resourceTemplates.length === 4,
    `resourceTemplates=${templates.resourceTemplates.length}, want 4`);

  const projectsResource = await client.readResource({ uri: "novel://projects" });
  assert(projectsResource.contents?.length === 1, "novel://projects 读取失败");

  const listed = await client.callTool({ name: "list_projects", arguments: {} });
  assert(!listed.isError && listed.structuredContent, "list_projects structuredContent 缺失");

  const created = await client.callTool({
    name: "create_project",
    arguments: { id: project, brief: "TypeScript MCP SDK 当前协议黑盒测试", style: "default" },
  });
  assert(!created.isError, "create_project 返回 isError");
  assert(created.structuredContent && typeof created.structuredContent === "object",
    "create_project structuredContent 缺失");
  createdRevision = created.structuredContent.revision;
  assert(typeof createdRevision === "string" && createdRevision.length === 64,
    "create_project revision 不合法");

  const completion = await client.complete({
    ref: { type: "ref/resource", uri: "novel://project/{project}/status" },
    argument: { name: "project", value: project.slice(0, 4) },
  });
  assert(completion.completion?.values?.includes(project), "resource project completion 缺少新建项目");

  const next = await client.callTool({ name: "next_step", arguments: { project } });
  assert(!next.isError && next.structuredContent, "next_step 调用失败");
  latestRevision = next.structuredContent.revision;
  const actions = next.structuredContent.result?.actions;
  assert(Array.isArray(actions) && actions.length > 0, "next_step.actions 缺失");
  for (const [index, action] of actions.entries()) {
    assert(action.id === `a${index + 1}`, "action.id 不稳定");
    assert(Array.isArray(action.depends_on), "action.depends_on 缺失");
    assert(typeof action.revision_source === "string", "action.revision_source 缺失");
    assert(action.mode === "required" || action.mode === "choice", "action.mode 不合法");
    assert(typeof action.choice_group === "string", "action.choice_group 缺失");
    assert(typeof action.requires_revision === "boolean", "action.requires_revision 缺失");
  }
  assert(actions[0].tool === "novel_guide" && actions[0].revision_source === "none",
    "开局首动作应为不消耗 revision 的 novel_guide");
  assert(actions[1].tool === "save_book" && actions[1].revision_source === "plan",
    "save_book 应使用 next_step envelope revision");
  assert(next.structuredContent.result?.context_resource_uri === `novel://project/${project}/context`,
    "next_step.context_resource_uri 不正确");

  const statusResource = await client.readResource({ uri: `novel://project/${project}/status` });
  assert(statusResource.contents?.length === 1, "项目 status resource 读取失败");

  console.log(JSON.stringify({
    ok: true,
    sdk: "@modelcontextprotocol/client@2.0.0",
    tools: tools.tools.length,
    prompts: prompts.prompts.length,
    resourceTemplates: templates.resourceTemplates.length,
    completions: completion.completion.values.length,
    project,
  }));
} finally {
  if (latestRevision || createdRevision) {
    try {
      await client.callTool({
        name: "delete_project",
        arguments: { project, expected_revision: latestRevision || createdRevision },
      });
    } catch {
      // 测试数据目录通常是临时目录；清理失败不遮蔽原始测试结果。
    }
  }
  await client.close().catch(() => {});
}
