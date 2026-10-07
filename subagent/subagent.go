// Package subagent lets an agent delegate a job to a fresh agent through the
// task tool. The child runs in its own session.md, in a numbered subfolder of
// its parent's session, and only its final answer comes back as the tool
// result. It sits above orchestrator because tool can't import orchestrator.
package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/model"
	"github.com/niconiahi/the-agent/orchestrator"
	"github.com/niconiahi/the-agent/sender"
	"github.com/niconiahi/the-agent/session"
	"github.com/niconiahi/the-agent/tool"
	"github.com/niconiahi/the-agent/vimtool"
)

const ROLE_EXPLORER = "explorer"

// EXPLORER_TOOLS can only read: an explorer can't change anything.
var EXPLORER_TOOLS = []string{"read", "grep", "find", "ls", "bash_read"}

const SLUG_WORDS = 4

// Host shows a child's session while the child runs.
type Host interface {
	// Open starts streaming into the session.md at path. Every event of the
	// child goes to listener; end is called once the child stops, with the
	// error it stopped on.
	Open(path string) (listener func(orchestrator.AgentEvent), end func(error), err error)
}

type Config struct {
	Model         *model.Model
	StreamOptions *sender.StreamOptions
	// Tools are the tools roles pick theirs from.
	Tools []tool.Tool
	// SystemPrompt is the path of the system_prompt.md every agent links to.
	SystemPrompt string
	Host         Host
	Now          func() time.Time
}

// Link is the Details of a task tool result: the child's folder, relative to
// the parent's session directory.
type Link struct {
	Folder string
}

// String is the markdown link from the parent's session.md to the child's.
func (link Link) String() string {
	return fmt.Sprintf("[%s](%s/session.md)", link.Folder, link.Folder)
}

const DESCRIPTION = `Delegate a job to an explorer subagent: a fresh agent with its own context that can only read (read, grep, find, ls, bash_read). Only its final answer comes back to you. The job is all it knows: say what to find out and exactly what to report, such as findings with file paths and line numbers.`

func Task(config Config) tool.Tool {
	parameters := json.RawMessage(`{
		"type": "object",
		"properties": {
			"job": {"type": "string", "description": "What the subagent should do and what it should report back"},
			"role": {"type": "string", "enum": ["explorer"], "description": "explorer reads only"}
		},
		"required": ["job"]
	}`)
	return tool.NewTool("task", DESCRIPTION, parameters, func(invocation_context context.Context, _ string, arguments map[string]any) (tool.ToolResult, error) {
		return config.run(invocation_context, arguments)
	})
}

func (config Config) run(invocation_context context.Context, arguments map[string]any) (tool.ToolResult, error) {
	job, _ := arguments["job"].(string)
	if strings.TrimSpace(job) == "" {
		return tool.ToolResult{}, errors.New("job is required")
	}
	if role, _ := arguments["role"].(string); role != "" && role != ROLE_EXPLORER {
		return tool.ToolResult{}, fmt.Errorf("unknown role %q: use explorer", role)
	}
	parent := vimtool.SessionDirectory(invocation_context)
	if parent == "" {
		return tool.ToolResult{}, errors.New("task runs only inside a session")
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}

	directory, error := create_folder(parent, job)
	if error != nil {
		return tool.ToolResult{}, error
	}
	path := filepath.Join(directory, "session.md")
	prompt, first, error := config.write_session(path, job, now())
	if error != nil {
		return tool.ToolResult{}, error
	}

	listener, end, error := config.Host.Open(path)
	if error != nil {
		return tool.ToolResult{}, error
	}
	child := orchestrator.New(
		orchestrator.WithID(directory),
		orchestrator.WithModel(config.Model),
		orchestrator.WithTools(Tools(config.Tools, EXPLORER_TOOLS)),
		orchestrator.WithSystemPrompt(prompt),
		orchestrator.WithStreamOptions(config.StreamOptions),
	)
	child.Subscribe(listener)
	error = child.Prompt(vimtool.WithSession(invocation_context, directory, now), first)
	end(error)
	if error != nil {
		return tool.ToolResult{}, fmt.Errorf("subagent %s: %w", filepath.Base(directory), error)
	}
	return tool.ToolResult{
		Content: []message.Content{message.TextContent{Text: report(child.State().Messages)}},
		Details: Link{Folder: filepath.Base(directory)},
	}, nil
}

// Tools picks the tools named in names, in the order of tools.
func Tools(tools []tool.Tool, names []string) []tool.Tool {
	picked := []tool.Tool{}
	for _, current := range tools {
		if slices.Contains(names, current.Name) {
			picked = append(picked, current)
		}
	}
	return picked
}

// write_session writes the child's session.md, linked to the shared
// system_prompt.md and holding the job as its first, timestamped user
// message. It returns the child's system prompt and that first message as
// the file reads, so the child starts exactly as a send of its file would.
func (config Config) write_session(path string, job string, at time.Time) (string, message.Message, error) {
	link, error := filepath.Rel(filepath.Dir(path), config.SystemPrompt)
	if error != nil {
		return "", nil, error
	}
	parsed, error := session.Parse(session.NewLinked(filepath.ToSlash(link), at) + strings.TrimSpace(job) + "\n")
	if error != nil {
		return "", nil, error
	}
	parsed.StampLastUser(at)
	if error := os.WriteFile(path, []byte(parsed.Render()), 0o644); error != nil {
		return "", nil, error
	}
	contents, error := os.ReadFile(config.SystemPrompt)
	if error != nil {
		return "", nil, fmt.Errorf("system prompt: %w", error)
	}
	messages := parsed.Messages()
	return parsed.SystemPrompt(string(contents)), messages[len(messages)-1], nil
}

// create_folder makes the next numbered subfolder of parent, named from job:
// 01-map-callers, 02-rewrite-middleware. os.Mkdir fails on a folder that
// exists, so tasks started at the same time never share one.
func create_folder(parent string, job string) (string, error) {
	entries, error := os.ReadDir(parent)
	if error != nil {
		return "", error
	}
	next := 1
	for _, entry := range entries {
		prefix, _, ok := strings.Cut(entry.Name(), "-")
		if number, error := strconv.Atoi(prefix); ok && error == nil && entry.IsDir() {
			next = max(next, number+1)
		}
	}
	name := slug(job)
	for {
		directory := filepath.Join(parent, fmt.Sprintf("%02d-%s", next, name))
		error := os.Mkdir(directory, 0o755)
		if error == nil {
			return directory, nil
		}
		if !errors.Is(error, os.ErrExist) {
			return "", error
		}
		next++
	}
}

func slug(job string) string {
	words := strings.FieldsFunc(strings.ToLower(job), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(words) == 0 {
		return "task"
	}
	return strings.Join(words[:min(len(words), SLUG_WORDS)], "-")
}

// report is the text of the child's last assistant message: its final answer.
func report(messages []message.Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		var reply message.AssistantMessage
		switch typed := messages[index].(type) {
		case message.AssistantMessage:
			reply = typed
		case *message.AssistantMessage:
			reply = *typed
		default:
			continue
		}
		parts := []string{}
		for _, content := range reply.Content {
			if text, ok := content.(message.TextContent); ok {
				parts = append(parts, text.Text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n\n"))
	}
	return ""
}
