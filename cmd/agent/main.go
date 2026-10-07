package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"

	neovim "github.com/neovim/go-client/nvim"
	"github.com/niconiahi/the-agent/clone"
	"github.com/niconiahi/the-agent/model"
	"github.com/niconiahi/the-agent/nvim"
	"github.com/niconiahi/the-agent/sender"
	"github.com/niconiahi/the-agent/setup"
	"github.com/niconiahi/the-agent/tool"
	"github.com/niconiahi/the-agent/vimtool"

	_ "github.com/niconiahi/the-agent/sender"
)

const SYSTEM_PROMPT = `You are a coding agent. You can read, write, and edit files. You can run shell commands with bash_read, which runs them as a user that can read the project but not write it, and with bash_write, which runs a command that must change files in a copy of the project and applies its changes as edits. You can search for files and their contents. Help the user with their coding tasks.`

const MAX_DEPTH_VARIABLE = "THE_AGENT_MAX_DEPTH"

const USAGE =`the-agent runs inside Neovim: Neovim starts it as "the-agent --nvim".

Install the plugin (see extras/lazy.lua for a lazy.nvim spec), then use
:TA <name> to open a session, :TASend to send it and :TAAbort to stop a turn.

Once per project, let the agent's shell commands run as _the-agent:

  sudo the-agent setup [--dry-run] [--check | --uninstall [--all]] [project]
`

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "setup" {
		os.Exit(run_setup(os.Args[2:]))
	}
	if len(os.Args) >= 2 && os.Args[1] == "sync" {
		os.Exit(run_sync(os.Args[2:]))
	}
	if len(os.Args) != 2 || os.Args[1] != "--nvim" {
		fmt.Fprint(os.Stderr, USAGE)
		os.Exit(2)
	}
	load_env(".env")
	run_nvim()
}

func default_tools(client *neovim.Nvim, project string, binary string) []tool.Tool {
	sandbox := tool.Sandbox{User: setup.USER, Home: setup.Home(), Project: project}
	tools := []tool.Tool{
		vimtool.Read(client),
		tool.BashReadTool(sandbox),
		vimtool.Edit(client),
		vimtool.Write(client),
		vimtool.Filter(client),
		vimtool.Grep(client),
		tool.FindTool(),
		tool.LsTool(),
	}
	if setup.Supported(runtime.GOOS) {
		clone := tool.Clone{Project: project, Binary: binary, Run: sandbox.Command}
		tools = append(tools, tool.BashWriteTool(sandbox, clone, vimtool.Replay(client)))
	}
	return tools
}

func run_nvim() {
	log.SetOutput(os.Stderr)

	target := model.KimiK25()
	api_key := os.Getenv("KIMI_API_KEY")
	project := working_directory()
	binary := binary_path()
	depth, depth_error := max_depth(os.Getenv(MAX_DEPTH_VARIABLE))
	config := nvim.Config{
		Project: project,
		Sandbox: func(project string) error {
			return setup.Ready(project, binary)
		},
		Model:         &target,
		SystemPrompt:  SYSTEM_PROMPT,
		StreamOptions: &sender.StreamOptions{APIKey: api_key},
		MaxDepth:      depth,
		Ready: func() error {
			if depth_error != nil {
				return depth_error
			}
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
	config.Tools = default_tools(client, project, binary)
	if error := nvim.Attach(client, config); error != nil {
		log.Fatalf("the-agent: %v", error)
	}

	if error := client.Serve(); error != nil {
		log.Fatalf("the-agent: %v", error)
	}
}

func max_depth(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	depth, error := strconv.Atoi(value)
	if error != nil || depth < 1 {
		return 0, fmt.Errorf("%s must be a positive whole number, got %q", MAX_DEPTH_VARIABLE, value)
	}
	return depth, nil
}

func binary_path() string {
	binary, error := setup.Binary()
	if error != nil {
		log.Fatalf("the-agent: %v", error)
	}
	return binary
}

func working_directory() string {
	directory, error := os.Getwd()
	if error != nil {
		log.Fatalf("the-agent: %v", error)
	}
	return directory
}

func run_setup(arguments []string) int {
	host, error := setup.Local(os.Stdout)
	if error == nil {
		error = setup.Run(host, arguments)
	}
	if error != nil {
		fmt.Fprintf(os.Stderr, "the-agent setup: %v\n", error)
		return 1
	}
	return 0
}

func run_sync(arguments []string) int {
	if error := clone.Run(arguments); error != nil {
		fmt.Fprintf(os.Stderr, "the-agent sync: %v\n", error)
		return 1
	}
	return 0
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
