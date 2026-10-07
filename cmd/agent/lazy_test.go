package main

import (
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/niconiahi/the-agent/nvim"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
)

// lazy_spec is extras/lazy.lua as lazy.nvim would read it.
type lazy_spec struct {
	Build string            `msgpack:"build"`
	Cmd   []string          `msgpack:"cmd"`
	Keys  map[string]string `msgpack:"keys"` // lhs -> rhs
	Remap bool              `msgpack:"remap"`
}

// The lazy.nvim spec builds the binary where the plugin looks for it by
// default, lazy-loads on the commands, and maps the keys to <Plug>s.
func TestLazySpec_BuildsTheBinaryAndLoadsOnCommands(t *testing.T) {
	harness := nvimtest.Launch(t)
	root := nvimtest.RepoRoot()

	var spec lazy_spec
	if error := harness.Nvim.ExecLua(`
		local spec = dofile(...)
		local keys, remap = {}, true
		for _, key in ipairs(spec.keys) do
			keys[key[1]] = key[2]
			remap = remap and key.remap == true
		end
		return { build = spec.build, cmd = spec.cmd, keys = keys, remap = remap }
	`, &spec, filepath.Join(root, "extras", "lazy.lua")); error != nil {
		t.Fatal(error)
	}

	for _, command := range []string{"TA", "TASend", "TAAbort"} {
		if !slices.Contains(spec.Cmd, command) {
			t.Errorf("spec does not lazy-load on :%s (cmd = %v)", command, spec.Cmd)
		}
	}
	want := map[string]string{
		"<leader>aa": "<Plug>(TA)",
		"<leader>as": "<Plug>(TASend)",
		"<leader>ax": "<Plug>(TAAbort)",
	}
	for lhs, rhs := range want {
		if spec.Keys[lhs] != rhs {
			t.Errorf("key %s: want %s, got %q", lhs, rhs, spec.Keys[lhs])
		}
	}
	if !spec.Remap {
		t.Error("<Plug> keys need remap = true")
	}

	// lazy.nvim runs a non-":" build string as a shell command in the plugin
	// directory.
	build := exec.Command("sh", "-c", spec.Build)
	build.Dir = root
	if output, error := build.CombinedOutput(); error != nil {
		t.Fatalf("build %q: %v\n%s", spec.Build, error, output)
	}

	// lazy.nvim calls setup(opts); with no bin override the plugin must find
	// what build produced.
	if error := harness.Nvim.ExecLua(`require("the-agent").setup(dofile(...).opts)`, nil, filepath.Join(root, "extras", "lazy.lua")); error != nil {
		t.Fatal(error)
	}
	harness.Command("TA foo")
	if got := harness.ReadFile(".the-agent/sessions/foo/session.md"); got != nvim.NEW_SESSION {
		t.Fatalf("session not created by the built binary: %q", got)
	}
}
