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
	"github.com/niconiahi/the-agent/tool"

	_ "github.com/niconiahi/the-agent/sender"
)

const SYSTEM_PROMPT = `You are a coding agent. You can read, write, and edit files. You can run bash commands. You can search for files and their contents. Help the user with their coding tasks.`

const USAGE = `the-agent runs inside Neovim: Neovim starts it as "the-agent --nvim".

Install the plugin (see extras/lazy.lua for a lazy.nvim spec), then use
:TA <name> to open a session, :TASend to send it and :TAAbort to stop a turn.
`

func main() {
	if len(os.Args) != 2 || os.Args[1] != "--nvim" {
		fmt.Fprint(os.Stderr, USAGE)
		os.Exit(2)
	}
	load_env(".env")
	run_nvim()
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
