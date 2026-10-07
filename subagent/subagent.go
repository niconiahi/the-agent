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

const (
	ROLE_EXPLORER = "explorer"
	ROLE_WORKER   = "worker"
)

const DEFAULT_MAX_DEPTH = 3

const SLUG_WORDS = 4

type Host interface {
	Open(path string) (listener func(orchestrator.AgentEvent), end func(error), error error)
}

type Config struct {
	Model         *model.Model
	StreamOptions *sender.StreamOptions
	Tools         []tool.Tool
	ExplorerTools []string
	MaxDepth      int
	SystemPrompt  string
	Host          Host
	Now           func() time.Time
}

type Link struct {
	Folder string
}

func (link Link) String() string {
	return fmt.Sprintf("[%s](%s/session.md)", link.Folder, link.Folder)
}

const DESCRIPTION = `Delegate a job to a subagent: a fresh agent with its own context. Only its final answer comes back to you. The job is all it knows: say what to do and exactly what to report, such as findings with file paths and line numbers, or a summary of the changes made. An explorer can only read (read, grep, find, ls, bash_read) and should do most jobs; a worker can also change files (edit, write, filter, bash_write). Give writing subagents disjoint sets of files. Several task calls in one turn run at the same time.`

func Task(config Config) tool.Tool {
	parameters := json.RawMessage(`{
		"type": "object",
		"properties": {
			"job": {"type": "string", "description": "What the subagent should do and what it should report back"},
			"role": {"type": "string", "enum": ["explorer", "worker"], "description": "explorer (the default) reads only; worker can also change files"}
		},
		"required": ["job"]
	}`)
	return tool.NewTool("task", DESCRIPTION, parameters, func(invocation_context context.Context, _ string, arguments map[string]any) (tool.ToolResult, error) {
		return config.run(invocation_context, arguments)
	})
}

func (config Config) explorer_tools() []string {
	if config.ExplorerTools != nil {
		return config.ExplorerTools
	}
	return []string{"read", "grep", "find", "ls", "bash_read"}
}

func (config Config) role_tools(role string) []string {
	if role == ROLE_WORKER {
		return append(slices.Clone(config.explorer_tools()), "edit", "write", "filter", "bash_write")
	}
	return config.explorer_tools()
}

func (config Config) max_depth() int {
	if config.MaxDepth > 0 {
		return config.MaxDepth
	}
	return DEFAULT_MAX_DEPTH
}

// tools is the tool set of an agent with role at depth: a root session
// (role "") gets every tool, a subagent its role's tools, and either gets
// task while it is below the depth limit.
func (config Config) tools(role string, depth int) []tool.Tool {
	tools := slices.Clone(config.Tools)
	if role != "" {
		tools = pick(config.Tools, config.role_tools(role))
	}
	if depth < config.max_depth() {
		tools = append(tools, Task(config))
	}
	return tools
}

// Depth is how many sessions directory is nested in: 0 for a root session,
// 1 for its subagents, and so on, since a session is nested in every
// ancestor folder that holds a session.md.
func Depth(directory string) int {
	depth := 0
	for parent := filepath.Dir(directory); parent != directory; directory, parent = parent, filepath.Dir(parent) {
		if _, error := os.Stat(filepath.Join(parent, "session.md")); error != nil {
			break
		}
		depth++
	}
	return depth
}

func (config Config) run(invocation_context context.Context, arguments map[string]any) (tool.ToolResult, error) {
	job, _ := arguments["job"].(string)
	if strings.TrimSpace(job) == "" {
		return tool.ToolResult{}, errors.New("job is required")
	}
	role, _ := arguments["role"].(string)
	if role == "" {
		role = ROLE_EXPLORER
	}
	if role != ROLE_EXPLORER && role != ROLE_WORKER {
		return tool.ToolResult{}, fmt.Errorf("unknown role %q: use explorer or worker", role)
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
		orchestrator.WithTools(config.tools(role, Depth(directory))),
		orchestrator.WithSystemPrompt(prompt),
		orchestrator.WithStreamOptions(config.StreamOptions),
		orchestrator.WithToolExecution(orchestrator.TOOL_EXECUTION_PARALLEL),
	)
	child.Subscribe(listener)
	error = child.Prompt(vimtool.WithSession(invocation_context, directory, now), first)
	end(error)
	link := Link{Folder: filepath.Base(directory)}
	if error != nil {
		return tool.ToolResult{Details: link}, fmt.Errorf("subagent %s: %w", link.Folder, error)
	}
	return tool.ToolResult{
		Content: []message.Content{message.TextContent{Text: report(child.State().Messages)}},
		Details: link,
	}, nil
}

func pick(tools []tool.Tool, names []string) []tool.Tool {
	picked := []tool.Tool{}
	for _, current := range tools {
		if slices.Contains(names, current.Name) {
			picked = append(picked, current)
		}
	}
	return picked
}

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
	words := strings.FieldsFunc(strings.ToLower(job), func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	})
	if len(words) == 0 {
		return "task"
	}
	return strings.Join(words[:min(len(words), SLUG_WORDS)], "-")
}

func report(messages []message.Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		switch typed := messages[index].(type) {
		case message.AssistantMessage:
			return strings.TrimSpace(message.Text(typed.Content))
		case *message.AssistantMessage:
			return strings.TrimSpace(message.Text(typed.Content))
		}
	}
	return ""
}
