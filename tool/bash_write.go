package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/niconiahi/the-agent/message"
)

type Change struct {
	Path    string
	Content []byte
	Mode    fs.FileMode
	Link    string
	Deleted bool
	Folder  bool
}

type Replay struct {
	Write  func(invocation_context context.Context, path string, content string) error
	Delete func(invocation_context context.Context, path string) error
}

func BashWriteTool(sandbox Sandbox, clone Clone, replay Replay) Tool {
	parameters := json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "The bash command that changes files, run in a copy of the project"},
			"timeout": {"type": "integer", "description": "Timeout in seconds"}
		},
		"required": ["command"]
	}`)

	description := "Run a bash command that must write the project, like go mod tidy or go generate. It runs as " + sandbox.User + " in a copy of the project at .the-agent/clone, brought up to date first; every file it creates, changes or deletes (outside .git) is then applied to the project as an edit. Use bash_read for anything that only reads."
	return NewTool("bash_write", description, parameters, func(invocation_context context.Context, _ string, arguments map[string]interface{}) (ToolResult, error) {
		unlock := clone.lock()
		defer unlock()

		before, error := clone.sync(invocation_context)
		if error != nil {
			return ToolResult{}, explain_refusal(sandbox, error)
		}

		result, error := run_bash(invocation_context, sandbox, clone.command, arguments)
		if error != nil {
			return result, error
		}
		if error := invocation_context.Err(); error != nil {
			return ToolResult{}, fmt.Errorf("bash_write stopped, nothing was applied: %w", error)
		}
		changes, error := clone.changes(invocation_context, before)
		if error != nil {
			return ToolResult{}, explain_refusal(sandbox, error)
		}

		text := result.Content[0].(message.TextContent).Text
		result.Content = []message.Content{message.TextContent{Text: text + apply_changes(invocation_context, sandbox.Project, changes, replay)}}
		return result, nil
	})
}

func explain_refusal(sandbox Sandbox, error error) error {
	var failure *step_failure
	if errors.As(error, &failure) && failure.exit_code == 1 && sudo_refused(failure.stderr) {
		return setup_error(sandbox, failure.stderr)
	}
	return error
}

type replayed_change struct {
	path    string
	line    string
	applied bool
}

func replay_order(changes []Change) {
	rank := func(change Change) int {
		switch {
		case change.Folder:
			return 1
		case change.Deleted:
			return 0
		}
		return 2
	}
	sort.Slice(changes, func(first int, second int) bool {
		if rank(changes[first]) != rank(changes[second]) {
			return rank(changes[first]) < rank(changes[second])
		}
		if changes[first].Folder {
			return changes[first].Path > changes[second].Path
		}
		return changes[first].Path < changes[second].Path
	})
}

func apply_changes(invocation_context context.Context, project string, changes []Change, replay Replay) string {
	replay_order(changes)
	results := []replayed_change{}
	for _, change := range changes {
		result, error := apply_change(invocation_context, project, change, replay)
		if error != nil {
			result = replayed_change{line: fmt.Sprintf("%s (%v)", change.Path, error)}
		}
		if result.line != "" {
			result.path = change.Path
			results = append(results, result)
		}
	}
	sort.SliceStable(results, func(first int, second int) bool { return results[first].path < results[second].path })

	applied := []string{}
	skipped := []string{}
	for _, result := range results {
		if result.applied {
			applied = append(applied, result.line)
		} else {
			skipped = append(skipped, result.line)
		}
	}
	if len(applied) == 0 && len(skipped) == 0 {
		return "\n\nNo files changed."
	}
	report := ""
	if len(applied) > 0 {
		report += "\n\nApplied to the project:\n" + strings.Join(applied, "\n")
	}
	if len(skipped) > 0 {
		report += "\n\nNot applied:\n" + strings.Join(skipped, "\n")
	}
	return report
}

func remove_folder(path string, relative string) (replayed_change, error) {
	info, error := os.Lstat(path)
	if errors.Is(error, fs.ErrNotExist) || (error == nil && !info.IsDir()) {
		return replayed_change{}, nil
	}
	if error != nil {
		return replayed_change{}, error
	}
	entries, error := os.ReadDir(path)
	if error != nil {
		return replayed_change{}, error
	}
	if len(entries) > 0 {
		return replayed_change{line: relative + "/ (folder not empty)"}, nil
	}
	if error := os.Remove(path); error != nil {
		return replayed_change{}, error
	}
	return replayed_change{line: "D " + relative + "/", applied: true}, nil
}

func apply_change(invocation_context context.Context, project string, change Change, replay Replay) (replayed_change, error) {
	if !filepath.IsLocal(change.Path) {
		return replayed_change{line: change.Path + " (outside the project)"}, nil
	}
	path := filepath.Join(project, change.Path)
	if change.Folder {
		return remove_folder(path, change.Path)
	}
	current, read_error := os.ReadFile(path)
	exists := read_error == nil

	switch {
	case change.Deleted:
		if _, error := os.Lstat(path); errors.Is(error, fs.ErrNotExist) {
			return replayed_change{}, nil
		}
		if error := replay.Delete(invocation_context, path); error != nil {
			return replayed_change{}, error
		}
		return replayed_change{line: "D " + change.Path, applied: true}, nil
	case change.Link != "":
		return replayed_change{line: change.Path + " (symbolic link)"}, nil
	case bytes.IndexByte(change.Content, 0) >= 0:
		return replayed_change{line: change.Path + " (binary file)"}, nil
	}

	if exists && bytes.Equal(current, change.Content) && same_permissions(path, change.Mode) {
		return replayed_change{}, nil
	}
	if !exists || !bytes.Equal(current, change.Content) {
		if error := replay.Write(invocation_context, path, string(change.Content)); error != nil {
			return replayed_change{}, error
		}
	}
	if !same_permissions(path, change.Mode) {
		if error := os.Chmod(path, change.Mode.Perm()); error != nil {
			return replayed_change{}, error
		}
	}
	if exists {
		return replayed_change{line: "M " + change.Path, applied: true}, nil
	}
	return replayed_change{line: "A " + change.Path, applied: true}, nil
}

func same_permissions(path string, mode fs.FileMode) bool {
	info, error := os.Stat(path)
	return error == nil && info.Mode().Perm() == mode.Perm()
}
