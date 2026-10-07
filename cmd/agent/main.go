package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	neovim "github.com/neovim/go-client/nvim"
	"github.com/niconiahi/the-agent/chat"
	"github.com/niconiahi/the-agent/model"
	"github.com/niconiahi/the-agent/nvim"
	"github.com/niconiahi/the-agent/orchestrator"
	"github.com/niconiahi/the-agent/sender"
	"github.com/niconiahi/the-agent/tool"

	_ "github.com/niconiahi/the-agent/sender"
)

const SYSTEM_PROMPT = `You are a coding agent. You can read, write, and edit files. You can run bash commands. You can search for files and their contents. Help the user with their coding tasks.`

func main() {
	load_env(".env")

	if len(os.Args) > 1 && os.Args[1] == "--nvim" {
		run_nvim()
		return
	}

	api_key := os.Getenv("KIMI_API_KEY")
	if api_key == "" {
		fmt.Fprintln(os.Stderr, "KIMI_API_KEY environment variable is required")
		os.Exit(1)
	}

	// TODO: the verbose mode is not supposed to write to a file
	// just as the chat goes, i want to see the reasoning and for it to stay there in the chat
	var verbose_file *os.File
	if os.Getenv("VERBOSE") != "" {
		var error error
		verbose_file, error = os.Create("agent-verbose.log")
		if error != nil {
			fmt.Fprintf(os.Stderr, "failed to create verbose log: %v\n", error)
			os.Exit(1)
		}
		defer verbose_file.Close()
	}

	target := model.KimiK25()
	tools := default_tools()

	agent := orchestrator.New(
		orchestrator.WithModel(&target),
		orchestrator.WithTools(tools),
		orchestrator.WithSystemPrompt(SYSTEM_PROMPT),
		orchestrator.WithStreamOptions(&sender.StreamOptions{
			APIKey: api_key,
		}),
	)

	chat_model := chat.New(agent, target.Name)

	program := tea.NewProgram(
		chat_model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	unsubscribe := chat.StartEventBridge(agent, program, verbose_file)
	defer unsubscribe()

	if _, error := program.Run(); error != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", error)
		os.Exit(1)
	}
}

func default_tools() []tool.Tool {
	return []tool.Tool{
		tool.ReadTool(),
		tool.BashTool(),
		tool.EditTool(),
		tool.WriteTool(),
		tool.GrepTool(),
		tool.FindTool(),
		tool.LsTool(),
	}
}

// run_nvim serves msgpack-RPC on stdio for the Neovim that started this
// process with jobstart(..., { rpc = true }). It returns when that Neovim
// exits. stdout is the RPC pipe, so nothing else may write to it.
func run_nvim() {
	log.SetOutput(os.Stderr)

	target := model.KimiK25()
	api_key := os.Getenv("KIMI_API_KEY")
	config := nvim.Config{
		Model:         &target,
		SystemPrompt:  SYSTEM_PROMPT,
		Tools:         default_tools(),
		StreamOptions: &sender.StreamOptions{APIKey: api_key},
		Ready: func() error {
			if api_key == "" {
				return errors.New("KIMI_API_KEY environment variable is required")
			}
			return nil
		},
	}

	client, error := neovim.New(os.Stdin, os.Stdout, os.Stdout, log.Printf)
	if error != nil {
		log.Fatalf("the-agent: %v", error)
	}
	if error := nvim.Attach(client, config); error != nil {
		log.Fatalf("the-agent: %v", error)
	}

	if error := client.Serve(); error != nil {
		log.Fatalf("the-agent: %v", error)
	}
}

func load_env(path string) {
	file, error := os.Open(path)
	if error != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		os.Setenv(key, value)
	}
}
