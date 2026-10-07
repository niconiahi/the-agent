package integration_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/niconiahi/the-agent/nvim/nvimtest"
	"github.com/niconiahi/the-agent/vimtool"
)

func publish_later_on_save(harness *nvimtest.Harness, path string, delay int, after string) {
	harness.T.Helper()
	code := `
		local path, delay, after = ...
		local namespace = vim.api.nvim_create_namespace("fake-checker")
		vim.api.nvim_create_autocmd("BufWritePost", {
			buffer = vim.fn.bufnr(path),
			once = true,
			callback = function(event)
				vim.defer_fn(function()
					vim.diagnostic.set(namespace, event.buf, vim.fn.json_decode(after))
				end, delay)
			end,
		})
	`
	if error := harness.Nvim.ExecLua(code, nil, path, delay, after); error != nil {
		harness.T.Fatal(error)
	}
}

func TestEdit_WithoutALanguageServerReturnsPromptlyWithNoDiagnostics(t *testing.T) {
	harness, provider := start_with_vimtool(t, call("tc_1", "edit", map[string]any{"path": "a.txt", "old_text": "two", "new_text": "TWO"}))
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "one\ntwo\n")
	harness.Command("edit " + path)
	publish_later_on_save(harness, path, 300, `[{"lnum": 1, "col": 0, "severity": 1, "message": "too late"}]`)

	started := time.Now()
	send_agent_turn(harness)
	elapsed := time.Since(started)

	results := tool_results(t, provider)
	if results[0].IsError || result_text(results[0]) != "Edited a.txt\n\n- two\n+ TWO\n" {
		t.Fatalf("edit result: %q", result_text(results[0]))
	}
	if elapsed >= vimtool.DIAGNOSTICS_WAIT {
		t.Fatalf("turn took %s", elapsed)
	}
}

func TestEdit_ASilentLanguageServerDoesNotStallTheTurn(t *testing.T) {
	harness, provider := start_with_vimtool(t, call("tc_1", "edit", map[string]any{"path": "a.go", "old_text": "two", "new_text": "TWO"}))
	path := filepath.Join(harness.Dir, "a.go")
	harness.WriteFile("a.go", "one\ntwo\n")
	harness.Command("edit " + path)
	attach_fake_language_server(harness, path, 10000, `[
		{"range": {"start": {"line": 1, "character": 0}, "end": {"line": 1, "character": 3}}, "severity": 1, "message": "too late"}
	]`)

	started := time.Now()
	send_agent_turn(harness)
	elapsed := time.Since(started)

	results := tool_results(t, provider)
	if results[0].IsError || result_text(results[0]) != "Edited a.go\n\n- two\n+ TWO\n" {
		t.Fatalf("edit result: %q", result_text(results[0]))
	}
	if elapsed < vimtool.DIAGNOSTICS_WAIT || elapsed > 3*vimtool.DIAGNOSTICS_WAIT {
		t.Fatalf("turn took %s", elapsed)
	}
}

func publish_on_save(harness *nvimtest.Harness, path string, before string, after string) {
	harness.T.Helper()
	code := `
		local path, before, after = ...
		local namespace = vim.api.nvim_create_namespace("fake-checker")
		local buffer = vim.fn.bufnr(path)
		vim.diagnostic.set(namespace, buffer, vim.fn.json_decode(before))
		vim.api.nvim_create_autocmd("BufWritePost", {
			buffer = buffer,
			once = true,
			callback = function(event)
				vim.diagnostic.set(namespace, event.buf, vim.fn.json_decode(after))
			end,
		})
	`
	if error := harness.Nvim.ExecLua(code, nil, path, before, after); error != nil {
		harness.T.Fatal(error)
	}
}

func attach_fake_language_server(harness *nvimtest.Harness, path string, delay int, diagnostics string) {
	harness.T.Helper()
	code := `
		local path, delay, diagnostics = ...
		local buffer = vim.fn.bufnr(path)
		vim.lsp.start({
			name = "fake",
			cmd = function(dispatchers)
				local closing = false
				local id = 0
				return {
					request = function(method, _, callback)
						id = id + 1
						local result = nil
						if method == "initialize" then
							result = { capabilities = { textDocumentSync = { openClose = true, change = 1, save = true } } }
						end
						vim.schedule(function()
							callback(nil, result)
						end)
						return true, id
					end,
					notify = function(method, params)
						if method == "textDocument/didSave" then
							vim.defer_fn(function()
								dispatchers.notification("textDocument/publishDiagnostics", {
									uri = params.textDocument.uri,
									diagnostics = vim.fn.json_decode(diagnostics),
								})
							end, delay)
						end
						return true
					end,
					is_closing = function()
						return closing
					end,
					terminate = function()
						closing = true
					end,
				}
			end,
		}, { bufnr = buffer })
	`
	if error := harness.Nvim.ExecLua(code, nil, path, delay, diagnostics); error != nil {
		harness.T.Fatal(error)
	}
	harness.WaitFor("the language server", func() bool {
		var ready bool
		check := `local clients = vim.lsp.get_clients({ bufnr = vim.fn.bufnr(...) }); return #clients > 0 and clients[1].initialized == true`
		if error := harness.Nvim.ExecLua(check, &ready, path); error != nil {
			harness.T.Fatal(error)
		}
		return ready
	})
}

func TestEdit_WaitsForAnAttachedLanguageServerToPublish(t *testing.T) {
	harness, provider := start_with_vimtool(t, call("tc_1", "edit", map[string]any{"path": "a.go", "old_text": "two", "new_text": "TWO"}))
	path := filepath.Join(harness.Dir, "a.go")
	harness.WriteFile("a.go", "one\ntwo\nthree\n")
	harness.Command("edit " + path)
	attach_fake_language_server(harness, path, 100, `[
		{"range": {"start": {"line": 1, "character": 0}, "end": {"line": 1, "character": 3}}, "severity": 1, "message": "undefined: TWO", "source": "fake"}
	]`)

	send_agent_turn(harness)

	results := tool_results(t, provider)
	want := "Edited a.go\n\n- two\n+ TWO\n\nDiagnostics in the edited region:\n2:1 error: undefined: TWO (fake)\n"
	if results[0].IsError || result_text(results[0]) != want {
		t.Fatalf("edit result: %q", result_text(results[0]))
	}
}

func TestEdit_ReportsOnlyNewDiagnosticsInsideTheEditedRegion(t *testing.T) {
	harness, provider := start_with_vimtool(t, call("tc_1", "edit", map[string]any{"path": "a.txt", "old_text": "two", "new_text": "TWO\nTWO AGAIN"}))
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "one\ntwo\nthree\nfour\n")
	harness.Command("edit " + path)
	publish_on_save(harness, path,
		`[
			{"lnum": 1, "col": 0, "severity": 2, "message": "already there"},
			{"lnum": 3, "col": 0, "severity": 1, "message": "far away"}
		]`,
		`[
			{"lnum": 0, "col": 0, "severity": 1, "message": "new but outside"},
			{"lnum": 1, "col": 0, "severity": 2, "message": "already there"},
			{"lnum": 2, "col": 4, "severity": 1, "message": "broken by the edit", "source": "checker"},
			{"lnum": 4, "col": 0, "severity": 1, "message": "far away"}
		]`)

	send_agent_turn(harness)

	results := tool_results(t, provider)
	want := "Edited a.txt\n\n- two\n+ TWO\n+ TWO AGAIN\n\nDiagnostics in the edited region:\n3:5 error: broken by the edit (checker)\n"
	if results[0].IsError || result_text(results[0]) != want {
		t.Fatalf("edit result: %q", result_text(results[0]))
	}
}
