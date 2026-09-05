-- Event-only Neovim adapter for zsh-git-inlay. It never renders or generates.
local M = {}

local defaults = {
  executable = "zsh-git-inlay",
  file_events = true,
  diagnostics = true,
  max_diagnostics = 1024,
}

local max_data_bytes = 256

local state = {
  config = vim.deepcopy(defaults),
  group = nil,
  diagnostics = {},
  session = nil,
}

local function session_id()
  local uv = vim.uv or vim.loop
  return string.format("%x-%x", uv.hrtime(), vim.fn.getpid())
end

local function buffer_context(buffer)
  if not vim.api.nvim_buf_is_valid(buffer) or vim.bo[buffer].buftype ~= "" then
    return nil
  end
  local path = vim.api.nvim_buf_get_name(buffer)
  if path == "" or #path > max_data_bytes or path:find("[%z\r\n]") then
    return nil
  end
  local directory = vim.fs.dirname(path)
  if not directory or directory == "" then
    return nil
  end
  return path, directory
end

local function emit(kind, buffer, fields)
  local path, directory = buffer_context(buffer)
  if not path then
    return
  end
  local command = {
    state.config.executable,
    "activity",
    "emit",
    "--cwd",
    directory,
    "--source",
    "editor",
    "--kind",
    kind,
    "--sensitivity",
    "private",
    "--data",
    "path=" .. path,
    "--data",
    "session_id=" .. state.session,
  }
  for _, field in ipairs(fields or {}) do
    command[#command + 1] = "--data"
    command[#command + 1] = field
  end
  pcall(vim.system, command, { detach = true, stderr = false, stdout = false, text = false })
end

local function safe_emit(kind, buffer, fields)
  pcall(emit, kind, buffer, fields)
end

local function diagnostic_fields(buffer)
  local counts = vim.diagnostic.count(buffer)
  local severity = vim.diagnostic.severity
  local limit = state.config.max_diagnostics
  local values = {
    math.min(counts[severity.ERROR] or 0, limit),
    math.min(counts[severity.WARN] or 0, limit),
    math.min(counts[severity.INFO] or 0, limit),
    math.min(counts[severity.HINT] or 0, limit),
  }
  local signature = table.concat(values, ":")
  if state.diagnostics[buffer] == signature then
    return nil
  end
  state.diagnostics[buffer] = signature
  return {
    "error_count=" .. values[1],
    "warning_count=" .. values[2],
    "info_count=" .. values[3],
    "hint_count=" .. values[4],
  }
end

function M.disable()
  if state.group then
    pcall(vim.api.nvim_del_augroup_by_id, state.group)
  end
  state.group = nil
  state.diagnostics = {}
end

function M.setup(options)
  M.disable()
  state.config = vim.tbl_deep_extend("force", vim.deepcopy(defaults), options or {})
  state.config.max_diagnostics = math.max(1, math.min(4096, tonumber(state.config.max_diagnostics) or defaults.max_diagnostics))
  state.session = session_id()
  state.group = vim.api.nvim_create_augroup("ZshGitInlayActivity", { clear = true })

  if state.config.file_events then
    vim.api.nvim_create_autocmd({ "BufReadPost", "BufNewFile" }, {
      group = state.group,
      callback = function(args)
        safe_emit("editor.file_opened", args.buf)
      end,
    })
    vim.api.nvim_create_autocmd("BufWritePost", {
      group = state.group,
      callback = function(args)
        safe_emit("editor.file_saved", args.buf)
      end,
    })
  end

  if state.config.diagnostics then
    vim.api.nvim_create_autocmd("DiagnosticChanged", {
      group = state.group,
      callback = function(args)
        local ok, fields = pcall(diagnostic_fields, args.buf)
        if ok and fields then
          safe_emit("lsp.diagnostics_changed", args.buf, fields)
        end
      end,
    })
  end
  return M
end

function M.config()
  return vim.deepcopy(state.config)
end

return M
