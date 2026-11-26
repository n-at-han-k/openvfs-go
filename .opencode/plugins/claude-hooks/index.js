import { execFile, spawn } from "node:child_process"
import { readFile } from "node:fs/promises"
import { resolve } from "node:path"
import { promisify } from "node:util"
import { Plugin } from "@opencode/plugin"

const exec = promisify(execFile)

const EVENTS = [
  "PreToolUse", "PostToolUse", "PostToolUseFailure", "PermissionRequest", "Notification",
  "UserPromptSubmit", "Stop", "StopFailure", "SubagentStart", "SubagentStop", "PreCompact",
  "PostCompact", "Elicitation", "ElicitationResult", "TeammateIdle", "TaskCompleted", "Setup",
  "InstructionsLoaded", "CwdChanged", "FileChanged", "ConfigChange", "WorktreeCreate",
  "WorktreeRemove", "SessionStart", "SessionEnd", "PostToolBatch", "TaskCreated",
  "PermissionDenied", "UserPromptExpansion", "MessageDisplay", "DirectoryAdded",
  "PreModelSwitch", "PostModelSwitch",
]

const STREAM = {
  "session.created": "SessionStart",
  "session.forked": "SessionStart",
  "session.deleted": "SessionEnd",
  "session.execution.failed": "StopFailure",
  "session.compaction.ended": "PostCompact",
  "session.compaction.failed": "PostCompact",
  "filesystem.changed": "FileChanged",
  "config.updated": "ConfigChange",
  "form.created": "Elicitation",
  "form.replied": "ElicitationResult",
  "permission.asked": "Notification",
  "credential.updated": "Notification",
  "credential.switched": "Notification",
  "experimental.mcp.event": "Notification",
  "plugin.updated": "Notification",
  "reference.updated": "DirectoryAdded",
  "session.text.ended": "MessageDisplay",
  "session.model.selected": "PostModelSwitch",
  "location.shutdown": "SessionEnd",
}

const TOOL_NAMES = {
  bash: "Bash", shell: "Bash", powershell: "PowerShell", read: "Read", edit: "Edit",
  write: "Write", multiedit: "MultiEdit", notebookedit: "NotebookEdit", glob: "Glob",
  grep: "Grep", webfetch: "WebFetch", websearch: "WebSearch", task: "Agent",
  agent: "Agent", skill: "Skill", todowrite: "TodoWrite", taskcreate: "TaskCreate",
  taskupdate: "TaskUpdate", codemode: "PostToolBatch",
}

const OBSERVATIONAL = new Set([
  "Notification", "StopFailure", "SubagentStart", "PostCompact", "InstructionsLoaded",
  "CwdChanged", "FileChanged", "DirectoryAdded", "MessageDisplay", "SessionEnd", "Setup",
])

function parse(text) {
  const value = String(text ?? "").trim()
  if (!value.startsWith("{") || !value.endsWith("}")) return {}
  try {
    return JSON.parse(value) ?? {}
  } catch {
    return {}
  }
}

function atPath(object, path) {
  return String(path).split(".").reduce((value, key) => value?.[key], object)
}

function interpolate(value, payload, cwd, allow, options = {}) {
  return String(value ?? "")
    .replaceAll("\\${", "\u0000")
    .replaceAll("${CLAUDE_PROJECT_DIR}", cwd)
    .replaceAll(/\$\{([^}]+)\}/g, (_, path) => {
      const found = atPath(payload, path)
      return typeof found === "string" ? found : JSON.stringify(found ?? "")
    })
    .replaceAll(/\$\{?(\w+)\}?/g, (whole, name) => {
      const allowed = allow?.includes(name) && (!options.allowed || options.allowed.includes(name))
      return allowed ? process.env[name] ?? "" : ""
    })
    .replaceAll("\u0000", "${")
}

function glob(pattern) {
  return new RegExp(`^${pattern.replace(/[.+^${}()|[\]\\]/g, "\\$&").replace(/\*\*/g, ".*").replace(/\*/g, "[^/]*")}$`)
}

function ifMatches(rule, event, tool, toolInput) {
  if (!rule) return true
  if (!["PreToolUse", "PostToolUse", "PostToolUseFailure", "PermissionRequest", "PermissionDenied"].includes(event)) return false
  const found = /^([\w-]+)(?:\((.*)\))?$/.exec(rule.trim())
  if (!found) return false
  const [, name, pattern] = found
  if ((TOOL_NAMES[tool?.toLowerCase()] ?? tool) !== (TOOL_NAMES[name.toLowerCase()] ?? name)) return false
  if (pattern === undefined) return true
  if (name === "Bash" || name === "PowerShell") {
    const command = String(toolInput?.command ?? "")
    if (/\$\(|`|\$\w/.test(command)) return true
    return command.split(/[;&|]/).some((part) => glob(pattern).test(part.trim().replace(/^\w+=[^\s]+ /, "")))
  }
  return [toolInput?.file_path, toolInput?.path, toolInput?.url, toolInput?.pattern, toolInput?.query]
    .filter(Boolean)
    .some((value) => glob(pattern).test(String(value)))
}

function matches(matcher, value, event) {
  if (matcher === undefined || matcher === "" || matcher === "*") return true
  if (value === undefined || value === null) return false

  const values = [String(value)]
  const alias = TOOL_NAMES[String(value).toLowerCase()]
  if (alias) values.push(alias)

  const narrow = event === "FileChanged" || event === "StopFailure"
  const exact = narrow ? /^[\w|]+$/.test(matcher) : /^[\w\- ,|]+$/.test(matcher)
  if (exact) {
    const choices = matcher.split(narrow ? "|" : /[|,]/).map((part) => part.trim())
    return values.some((item) => choices.includes(item))
  }
  const regex = new RegExp(matcher)
  return values.some((item) => regex.test(item))
}

function base(event, extra = {}, sessionID = "") {
  return { ...extra, session_id: sessionID, cwd: process.cwd(), hook_event_name: event }
}

async function commandHook(hook, payload, ctx) {
  const run = () => new Promise((done, fail) => {
    const args = hook.args?.map((value) => interpolate(value, payload, process.cwd()))
    const child = spawn(interpolate(hook.command, payload, process.cwd()), args ?? [], {
      cwd: process.cwd(),
      shell: args === undefined ? hook.shell === "powershell" ? "powershell.exe" : true : false,
      timeout: (hook.timeout ?? 600) * 1000,
      env: {
        ...process.env,
        CLAUDE_PROJECT_DIR: process.cwd(),
        CLAUDE_ENV_FILE: `/tmp/claude-hooks-env-${process.pid}`,
      },
    })
    let stdout = ""
    let stderr = ""
    child.stdout.on("data", (chunk) => (stdout += chunk))
    child.stderr.on("data", (chunk) => (stderr += chunk))
    child.stdin.on("error", () => {})
    child.on("error", fail)
    child.on("close", (code) => done({ code: code ?? 1, stdout, stderr }))
    child.stdin.end(JSON.stringify(payload))
  })

  if (hook.async || hook.asyncRewake) {
    void run().then((result) => {
      const output = parse(result.stdout)
      if (hook.asyncRewake && result.code === 2 && ctx.session?.synthetic && payload.session_id) {
        const text = result.stderr.trim() || result.stdout.trim()
        if (text) void ctx.session.synthetic({ sessionID: payload.session_id, text })
      } else if (result.code === 0 && ctx.session?.synthetic && payload.session_id) {
        const text = output.additionalContext ?? output.systemMessage
        if (text) void ctx.session.synthetic({ sessionID: payload.session_id, text })
      }
    }).catch((error) => console.error("claude-hooks background:", error))
    return { code: 0, stdout: "", stderr: "", async: true }
  }

  return run()
}

async function httpHook(hook, payload, options) {
  const headers = {}
  for (const [name, value] of Object.entries(hook.headers ?? {})) {
    headers[name] = interpolate(value, payload, process.cwd(), hook.allowedEnvVars, options)
  }
  try {
    const response = await fetch(hook.url, {
      method: "POST",
      headers: { "Content-Type": "application/json", ...headers },
      body: JSON.stringify(payload),
      signal: AbortSignal.timeout((hook.timeout ?? 600) * 1000),
    })
    const stdout = await response.text()
    return response.ok
      ? { code: 0, stdout, stderr: "" }
      : { code: 1, stdout, stderr: `HTTP ${response.status}` }
  } catch (error) {
    return { code: 1, stdout: "", stderr: error.message }
  }
}

async function mcpHook(hook, payload, ctx) {
  try {
    const tools = await ctx.tool.list()
    const tool = tools.find((item) => item.id === `mcp__${hook.server}__${hook.tool}`)
      ?? tools.find((item) => item.id.endsWith(`__${hook.tool}`))
    if (!tool) return { code: 1, stdout: "", stderr: `MCP tool ${hook.server}/${hook.tool} not found` }
    const args = parse(interpolate(JSON.stringify(hook.input ?? {}), payload, process.cwd()))
    const result = await tool.execute(args, {
      signal: AbortSignal.timeout((hook.timeout ?? 600) * 1000),
      progress: async () => {},
    })
    return {
      code: result.isError ? 1 : 0,
      stdout: (result.content ?? []).map((part) => part.text ?? "").join("\n"),
      stderr: "",
    }
  } catch (error) {
    return { code: 1, stdout: "", stderr: error.message }
  }
}

function hookModel(hook, fallback) {
  if (!hook.model) return fallback
  if (typeof hook.model !== "string") return hook.model
  const [providerID, ...rest] = hook.model.split("/")
  return rest.length ? { providerID, id: rest.join("/") } : { ...fallback, id: hook.model }
}

function hookPrompt(hook, payload) {
  const json = JSON.stringify(payload)
  return hook.prompt?.includes("$ARGUMENTS")
    ? hook.prompt.replaceAll("$ARGUMENTS", json)
    : `${hook.prompt ?? ""}\n\n${json}`
}

async function modelHook(hook, payload, ctx) {
  try {
    const model = hookModel(hook, await ctx.model.default())
    const prompt = hookPrompt(hook, payload)
    if (hook.type === "prompt") {
      const result = await ctx.experimental.generate.text({ model, prompt })
      return { code: 0, stdout: result.text ?? "", stderr: "" }
    }

    const session = await ctx.session.create({ title: "Claude hook agent" })
    try {
      await ctx.session.switchModel({ sessionID: session.sessionID, model })
      await ctx.session.prompt({ sessionID: session.sessionID, text: prompt })
      await ctx.session.wait({ sessionID: session.sessionID })
      const messages = await ctx.session.context({ sessionID: session.sessionID })
      const last = messages.filter((message) => message.role === "assistant").at(-1)
      return { code: 0, stdout: (last?.parts ?? last?.content ?? []).map((part) => part.text ?? "").join("\n"), stderr: "" }
    } finally {
      await ctx.session.remove({ sessionID: session.sessionID })
    }
  } catch (error) {
    return { code: 1, stdout: "", stderr: error.message }
  }
}

async function invoke(hook, payload, ctx, options) {
  if (hook.type === "command") return commandHook(hook, payload, ctx)
  if (hook.type === "http") return httpHook(hook, payload, options)
  if (hook.type === "mcp_tool") return mcpHook(hook, payload, ctx)
  if (hook.type === "prompt" || hook.type === "agent") return modelHook(hook, payload, ctx)
  return { code: 1, stdout: "", stderr: `Unknown hook type ${hook.type}` }
}

function updatesFor(event, output, result) {
  const specific = output.hookSpecificOutput ?? {}
  const updates = {}
  if (event === "PreToolUse") {
    if (specific.updatedInput !== undefined) updates.updatedInput = specific.updatedInput
    if (specific.permissionDecision) updates.permissionDecision = specific.permissionDecision
    if (specific.permissionDecisionReason) updates.permissionDecisionReason = specific.permissionDecisionReason
    if (output.decision === "block" || result.code === 2) updates.permissionDecision = "deny"
  }
  if (event === "PermissionRequest") {
    updates.behavior = output.decision?.behavior
    updates.message = output.decision?.message
    updates.updatedInput = output.decision?.updatedInput
  }
  if (event === "PostToolUse") {
    updates.updatedToolOutput = specific.updatedToolOutput
    updates.updatedMCPToolOutput = specific.updatedMCPToolOutput
  }
  if (event === "PermissionDenied") updates.retry = output.retry === true
  if (event === "Elicitation" || event === "ElicitationResult") {
    updates.action = specific.action ?? output.action
    updates.content = specific.content
  }
  if (event === "WorktreeCreate") updates.worktreePath = output.worktreePath
  updates.additionalContext ??= specific.additionalContext ?? output.additionalContext
  updates.systemMessage = output.systemMessage
  return updates
}

async function run(event, hooks, payload, ctx, options, matcherValue) {
  const groups = hooks[event] ?? []
  if (!groups.length) return { reason: null, output: {}, updates: {}, text: "" }

  const jobs = []
  for (const group of groups) {
    if (!matches(group.matcher, matcherValue ?? payload.tool_name, event)) continue
    for (const hook of group.hooks ?? []) {
      if (!ifMatches(hook.if, event, payload.tool_name, payload.tool_input)) continue
      jobs.push({ hook, promise: invoke(hook, payload, ctx, options) })
    }
  }

  const results = []
  for (const job of jobs) results.push({ hook: job.hook, ...await job.promise })

  let reason = null
  let output = {}
  let text = ""
  const updates = {}

  for (const { hook, code, stdout, stderr } of results) {
    const current = parse(stdout)
    const specific = current.hookSpecificOutput ?? {}
    output = current
    text = stdout
    Object.assign(updates, updatesFor(event, current, { code }))

    const failed = code !== 0 && code !== 2
    if (failed && hook.onFailure === "block" && !reason) reason = stderr.trim() || `Claude ${event} hook failed`

    const blocked = code === 2 || current.continue === false || current.decision === "block"
      || specific.permissionDecision === "deny" || (hook.type === "prompt" && current.ok === false && !hook.continueOnBlock)
      || (hook.type === "agent" && current.ok === false)

    const allowsStop = ["Stop", "SubagentStop"].includes(event) && code === 2 && current.continue === false
    if (blocked && !allowsStop && !reason && !OBSERVATIONAL.has(event)) {
      reason = stderr.trim() || current.reason || current.stopReason ||
        specific.permissionDecisionReason || `Claude ${event} hook blocked`
    }
  }

  return { reason, output, updates, text }
}

export default Plugin.define({
  id: "claude-hooks",
  async setup(ctx) {
    const cwd = ctx.location.directory
    const settings = JSON.parse(
      await readFile(resolve(cwd, ctx.options?.config ?? ".claude/settings.json"), "utf8").catch(() => "{}"),
    )
    if (settings.disableAllHooks) return

    const hooks = settings.hooks ?? {}
    const options = { allowed: ctx.options?.httpHookAllowedEnvVars }
    const has = (event) => (hooks[event]?.length ?? 0) > 0
    const controller = new AbortController()
    const continued = new Set()
    const sessions = new Map()
    const lastMessage = new Map()

    if (has("Setup")) {
      await run("Setup", hooks, base("Setup", { trigger: "init" }), ctx, options)
    }

    if (["PreToolUse", "PostToolUse", "PostToolUseFailure", "PostToolBatch", "TaskCreated", "TaskCompleted"].some(has)) {
      await ctx.tool.hook("execute.before", async (event) => {
        if (!has("PreToolUse")) return
        const result = await run("PreToolUse", hooks, base("PreToolUse", {
          tool_name: event.tool, tool_input: event.input, tool_use_id: event.id,
        }, event.sessionID), ctx, options, event.tool)
        if (result.reason) throw new Error(result.reason)
        if (result.updates.permissionDecision === "deny") {
          throw new Error(result.updates.permissionDecisionReason ?? `Denied ${event.tool}`)
        }
        if (result.updates.updatedInput !== undefined) event.input = result.updates.updatedInput
      })

      await ctx.tool.hook("execute.after", async (event) => {
        const completed = event.status === "completed"
        const name = completed ? "PostToolUse" : "PostToolUseFailure"
        if (has(name)) {
          const result = await run(name, hooks, base(name, {
            tool_name: event.tool, tool_input: event.input, tool_use_id: event.id,
            tool_response: completed ? event.result : undefined,
            error: completed ? undefined : event.error,
          }, event.sessionID), ctx, options, event.tool)
          if (result.reason) throw new Error(result.reason)
          if (result.updates.updatedToolOutput !== undefined) event.result = result.updates.updatedToolOutput
        }

        if (completed && event.tool === "codemode" && has("PostToolBatch")) {
          const result = await run("PostToolBatch", hooks, base("PostToolBatch", {
            tool_calls: event.input?.tool_calls ?? [],
          }, event.sessionID), ctx, options)
          if (result.reason) throw new Error(result.reason)
        }

        if (completed && /^(task|todowrite|taskcreate)$/i.test(event.tool) && has("TaskCreated")) {
          const result = await run("TaskCreated", hooks, base("TaskCreated", {
            task_id: event.id,
            task_subject: event.input?.subject ?? event.input?.content ?? "",
          }, event.sessionID), ctx, options)
          if (result.reason) throw new Error(result.reason)
        }

        if (completed && /^(taskupdate|todowrite)$/i.test(event.tool) && has("TaskCompleted")) {
          if (String(event.input?.status ?? event.input?.state).toLowerCase() === "completed") {
            const result = await run("TaskCompleted", hooks, base("TaskCompleted", {
              task_id: event.id, task_subject: event.input?.subject ?? "",
            }, event.sessionID), ctx, options)
            if (result.reason) throw new Error(result.reason)
          }
        }
      })
    }

    if (has("UserPromptSubmit")) {
      await ctx.session.hook("prompt", async (event) => {
        const result = await run("UserPromptSubmit", hooks, base("UserPromptSubmit", {
          prompt: event.prompt.text,
        }, event.sessionID), ctx, options)
        if (result.reason) throw new Error(result.reason)
        if (result.updates.additionalContext) {
          event.prompt.text += `\n\n${result.updates.additionalContext}`
        }
      })
    }

    if (has("InstructionsLoaded") || has("PreCompact")) {
      await ctx.session.hook("context", async (event) => {
        if (has("InstructionsLoaded")) {
          for (const part of event.system ?? []) {
            for (const source of part.metadata?.experimental?.instructions ?? []) {
              await run("InstructionsLoaded", hooks, base("InstructionsLoaded", {
                file_path: source.key, memory_type: "Project", load_reason: "session_start",
              }, event.sessionID), ctx, options, "session_start")
            }
          }
        }
        if (has("PreCompact")) {
          const result = await run("PreCompact", hooks, base("PreCompact", {
            trigger: "auto", custom_instructions: null,
          }, event.sessionID), ctx, options, "auto")
          if (result.reason) throw new Error(result.reason)
          if (result.text) event.system.push({ type: "text", text: result.text })
        }
      })
    }

    if (has("PreModelSwitch")) {
      await ctx.session.hook("experimental.model.resolve", async (event) => {
        const result = await run("PreModelSwitch", hooks, base("PreModelSwitch", {
          from_model: event.model, to_model: event.model, requested_model: event.model, source: "command",
        }, event.sessionID), ctx, options, event.model.id)
        if (result.reason || result.updates.permissionDecision === "deny") {
          throw new Error(result.reason ?? result.updates.permissionDecisionReason ?? "Model switch denied")
        }
      })
    }

    if (has("PermissionRequest") || has("PermissionDenied")) {
      await ctx.permission.hook("evaluate", async (event) => {
        const name = event.effect === "deny" ? "PermissionDenied" : "PermissionRequest"
        if (!has(name) || (name === "PermissionRequest" && event.effect !== "ask")) return
        const result = await run(name, hooks, base(name, {
          tool_name: event.action, tool_input: event.metadata?.input ?? {},
        }, event.sessionID), ctx, options, event.action)
        if (name === "PermissionRequest" && ["allow", "deny"].includes(result.updates.behavior)) {
          event.effect = result.updates.behavior
        }
      })
    }

    if (has("CwdChanged")) {
      let last = cwd
      await ctx.shell.hook("create.before", async (event) => {
        if (event.cwd === last) return
        await run("CwdChanged", hooks, base("CwdChanged", {
          old_cwd: last, new_cwd: event.cwd, cwd: event.cwd,
        }), ctx, options)
        last = event.cwd
      })
    }

    if (has("UserPromptExpansion")) {
      const commands = await ctx.command.list()
      await ctx.command.transform((editor) => {
        for (const command of commands) {
          editor.add({
            ...command,
            execute: async (invocation) => {
              const result = await run("UserPromptExpansion", hooks, base("UserPromptExpansion", {
                expansion_type: "slash_command", command_name: command.name,
                command_args: invocation.prompt.text, prompt: invocation.prompt.text,
              }, invocation.sessionID), ctx, options, command.name)
              if (result.reason) throw new Error(result.reason)
              await command.execute(invocation)
            },
          })
        }
      })
    }

    if (has("WorktreeCreate") || has("WorktreeRemove")) {
      await ctx.worktree.transform((editor) => {
        editor.add({
          id: "claude-hooks",
          create: async (input) => {
            const result = await run("WorktreeCreate", hooks, base("WorktreeCreate", {
              name: input.directory.split("/").pop(),
            }), ctx, options)
            if (result.reason) throw new Error(result.reason)
            let directory = result.updates.worktreePath ?? result.text.trim().split("\n").filter(Boolean).at(-1)
            if (!directory) {
              await exec("git", ["-C", cwd, "worktree", "add", input.directory, "HEAD"])
              directory = input.directory
            }
            directory = resolve(cwd, directory)
            if (directory.includes("..")) throw new Error(`Unsafe worktree path: ${directory}`)
            return { directory }
          },
          remove: async (input) => {
            const result = await run("WorktreeRemove", hooks, base("WorktreeRemove", {
              worktree_path: input.directory,
            }), ctx, options)
            if (result.reason) throw new Error(result.reason)
            await exec("git", ["-C", cwd, "worktree", "remove", ...(input.force ? ["--force"] : []), input.directory])
          },
          list: async (source) => {
            const { stdout } = await exec("git", ["-C", source, "worktree", "list", "--porcelain"])
            const entries = []
            for (const line of stdout.split("\n")) {
              if (line.startsWith("worktree ")) {
                entries.push({ directory: line.slice(9), type: entries.length ? "worktree" : "root" })
              }
            }
            return entries
          },
        })
      })
    }

    if (["Stop", "SubagentStop", "TeammateIdle", "Notification"].some(has)) {
      await ctx.session.hook("experimental.idle", async (event) => {
        const sessionID = event.sessionID
        let info
        try {
          info = await ctx.session.get({ sessionID })
        } catch {}

        if (has("Notification")) {
          await run("Notification", hooks, base("Notification", {
            notification_type: "idle_prompt", message: "Waiting for input",
          }, sessionID), ctx, options, "idle_prompt")
        }

        const name = info?.parentID && has("SubagentStop") ? "SubagentStop"
          : info?.parentID && has("TeammateIdle") ? "TeammateIdle"
          : "Stop"
        if (!has(name)) return
        const result = await run(name, hooks, base(name, {
          stop_hook_active: continued.has(sessionID),
          last_assistant_message: lastMessage.get(sessionID) ?? "",
          background_tasks: event.background ?? [],
          teammate_name: info?.agent ?? "",
          agent_id: sessionID,
          agent_type: info?.agent ?? "",
        }, sessionID), ctx, options)
        const reason = result.reason ?? result.updates.additionalContext
        if (!reason) {
          continued.delete(sessionID)
          return
        }
        continued.add(sessionID)
        await ctx.session.synthetic({ sessionID, text: reason })
      })
    }

    await ctx.tool.transform((editor) => {
      editor.add({
        name: "claude_hooks_dispatch",
        description: "Dispatch any configured Claude Code hook event",
        input: {
          type: "object",
          properties: { event: { type: "string", enum: EVENTS }, input: { type: "object" } },
          required: ["event"],
        },
        execute: async ({ event, input: extra }) => {
          const result = await run(event, hooks, base(event, extra ?? {}, extra?.session_id ?? ""), ctx, options)
          if (result.reason) throw new Error(result.reason)
          return { content: JSON.stringify(result.updates) }
        },
      })
    })

    void (async () => {
      for await (const event of ctx.event.subscribe({ signal: controller.signal })) {
        const data = event.data ?? {}
        const name = event.type === "session.created" && data.parentID && has("SubagentStart")
          ? "SubagentStart"
          : STREAM[event.type]
        if (!name || !has(name)) continue
        const sessionID = data.sessionID ?? ""
        const payload = base(name, streamPayload(event.type, data, sessions, lastMessage, cwd), sessionID)
        const matcher = data.file ?? data.reason ?? data.model?.id ?? data.notification_type
        const result = await run(name, hooks, payload, ctx, options, matcher)
        if (result.reason && sessionID) {
          await ctx.session.synthetic({ sessionID, text: result.reason })
        }
      }
    })().catch((error) => console.error("claude-hooks:", error))

    return () => controller.abort()
  },
})

function streamPayload(type, data, sessions, lastMessage, cwd) {
  if (type === "session.created") {
    sessions.set(data.sessionID, { parentID: data.parentID, agent: data.agent })
    return {
      source: data.parentID ? "fork" : "startup",
      model: data.model,
      agent_id: data.sessionID,
      agent_type: data.agent ?? "",
    }
  }
  if (type === "session.forked") return { source: "fork" }
  if (type === "session.deleted" || type === "location.shutdown") return { reason: "other" }
  if (type === "session.execution.failed") return { error: data.error?.message ?? "unknown", error_details: data.error }
  if (type === "session.compaction.ended" || type === "session.compaction.failed") {
    return { trigger: data.reason ?? "auto", compact_summary: data.text ?? "" }
  }
  if (type === "filesystem.changed") {
    return { file_path: data.file, event: data.event, cwd }
  }
  if (type === "config.updated") return { source: "project_settings" }
  if (type === "form.created") return { mcp_server_name: data.form?.server ?? "", message: data.form?.message ?? "" }
  if (type === "form.replied") return { mcp_server_name: data.answer?.server ?? "", action: data.answer?.action }
  if (type === "permission.asked") {
    return { notification_type: "permission_prompt", message: data.message ?? `Permission: ${data.action}` }
  }
  if (type.startsWith("credential.")) {
    return {
      notification_type: type === "credential.updated" ? "auth_success" : "auth_storage_failure",
      message: type,
    }
  }
  if (type === "reference.updated") return { directory: data.reference?.path ?? "", source: "register_repo_root" }
  if (type === "session.text.ended") {
    lastMessage.set(data.sessionID, data.text ?? "")
    return { turn_id: data.sessionID, message_id: data.assistantMessageID, index: data.ordinal ?? 0, final: true, delta: data.text ?? "" }
  }
  if (type === "session.model.selected") {
    return { from_model: data.model, to_model: data.model, requested_model: data.model, source: "command" }
  }
  if (type === "experimental.mcp.event" || type === "plugin.updated") {
    return { notification_type: "plugin_notification", message: data.name ?? type }
  }
  return data
}
