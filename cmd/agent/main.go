package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	neovim "github.com/neovim/go-client/nvim"
	"github.com/niconiahi/the-agent/model"
	"github.com/niconiahi/the-agent/nvim"
	"github.com/niconiahi/the-agent/sender"
	"github.com/niconiahi/the-agent/setup"
	"github.com/niconiahi/the-agent/tool"
	"github.com/niconiahi/the-agent/vimtool"

	_ "github.com/niconiahi/the-agent/sender"
)

const SYSTEM_PROMPT = `You are a coding agent. You can read, write, and edit files. You can run bash commands. You can search for files and their contents. Help the user with their coding tasks.`

const USAGE = `the-agent runs inside Neovim: Neovim starts it as "the-agent --nvim".

Install the plugin (see extras/lazy.lua for a lazy.nvim spec), then use
:TA <name> to open a session, :TASend to send it and :TAAbort to stop a turn.

Once per project, let the agent's shell commands run as _the-agent:

  sudo the-agent setup [--dry-run] [--check | --uninstall [--all]] [project]
`

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "setup" {
		os.Exit(run_setup(os.Args[2:]))
	}
	if len(os.Args) != 2 || os.Args[1] != "--nvim" {
		fmt.Fprint(os.Stderr, USAGE)
		os.Exit(2)
	}
	load_env(".env")
	run_nvim()
}

func default_tools(client *neovim.Nvim) []tool.Tool {
	return []tool.Tool{
		vimtool.Read(client),
		tool.BashTool(),
		vimtool.Edit(client),
		vimtool.Write(client),
		vimtool.Filter(client),
		vimtool.Grep(client),
		tool.FindTool(),
		tool.LsTool(),
	}
}

func run_nvim() {
	log.SetOutput(os.Stderr)

	target := model.KimiK25()
	api_key := os.Getenv("KIMI_API_KEY")
	config := nvim.Config{
		Model:         &target,
		SystemPrompt:  SYSTEM_PROMPT,
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
	config.Tools = default_tools(client)
	if error := nvim.Attach(client, config); error != nil {
		log.Fatalf("the-agent: %v", error)
	}

	if error := client.Serve(); error != nil {
		log.Fatalf("the-agent: %v", error)
	}
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
