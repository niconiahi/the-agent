package setup

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const USER = "_the-agent"
const SUDOERS = "/etc/sudoers.d/the-agent"
const SUDOERS_DRAFT = SUDOERS + ".tmp"
const SYSTEM_DARWIN = "darwin"
const SYSTEM_LINUX = "linux"

var CACHES = []string{"gocache", "gomodcache", "tmp", "clones"}

var ERROR_NOT_ROOT = errors.New("setup changes system users, sudoers and ACLs: run it as sudo the-agent setup, or add --dry-run to see the commands")
var ERROR_ALL_WITHOUT_UNINSTALL = errors.New("--all only goes with --uninstall")

const USAGE = "usage: sudo the-agent setup [--dry-run] [--check | --uninstall [--all]] [project]"

var ERROR_UNSUPPORTED_SYSTEM = errors.New("setup supports macOS and Linux only")

type Host struct {
	System    string
	Invoker   string
	Home      string
	Directory string
	Root      bool
	Shell     Shell
	Output    io.Writer
}

type Shell interface {
	Run(command Command) (string, error)
}

type Command struct {
	Args  []string
	Input string
}

func (command Command) String() string {
	words := make([]string, len(command.Args))
	for index, argument := range command.Args {
		words[index] = quote(argument)
	}
	line := strings.Join(words, " ")
	if command.Input != "" {
		line += " <<< " + quote(strings.TrimSuffix(command.Input, "\n"))
	}
	return line
}

func quote(word string) string {
	if word != "" && strings.Trim(word, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-") == "" {
		return word
	}
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

func command(arguments ...string) Command {
	return Command{Args: arguments}
}

type options struct {
	dry_run   bool
	check     bool
	uninstall bool
	all       bool
	project   string
}

func Run(host Host, arguments []string) error {
	parsed, error := parse(arguments)
	if error != nil {
		return error
	}
	target, error := platform_for(host.System)
	if error != nil {
		return error
	}
	if resolved, error := filepath.EvalSymlinks(host.Home); error == nil {
		host.Home = resolved
	}
	machine := &machine{host: host, platform: target, dry_run: parsed.dry_run}
	if !parsed.check && !parsed.dry_run && !host.Root {
		return ERROR_NOT_ROOT
	}
	if parsed.all {
		return machine.uninstall_all()
	}
	project, error := resolve_project(host, parsed.project)
	if error != nil {
		return error
	}
	switch {
	case parsed.check:
		return machine.check(project)
	case parsed.uninstall:
		return machine.uninstall(project)
	}
	return machine.install(project)
}

func parse(arguments []string) (options, error) {
	var parsed options
	flags := flag.NewFlagSet("the-agent setup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&parsed.dry_run, "dry-run", false, "print every command without running it")
	flags.BoolVar(&parsed.check, "check", false, "verify _the-agent can read the project")
	flags.BoolVar(&parsed.uninstall, "uninstall", false, "undo setup for the project")
	flags.BoolVar(&parsed.all, "all", false, "with --uninstall, remove everything")
	if error := flags.Parse(arguments); error != nil {
		return parsed, fmt.Errorf("%v\n%s", error, USAGE)
	}
	if flags.NArg() > 1 {
		return parsed, fmt.Errorf("setup takes one project directory, got %d", flags.NArg())
	}
	if parsed.all && !parsed.uninstall {
		return parsed, ERROR_ALL_WITHOUT_UNINSTALL
	}
	parsed.project = flags.Arg(0)
	return parsed, nil
}

func resolve_project(host Host, argument string) (string, error) {
	project := argument
	if project == "" {
		project = host.Directory
	}
	if !filepath.IsAbs(project) {
		project = filepath.Join(host.Directory, project)
	}
	resolved, error := filepath.EvalSymlinks(project)
	if error != nil {
		return "", fmt.Errorf("refusing %s: it is not a directory", project)
	}
	info, error := os.Stat(resolved)
	if error != nil || !info.IsDir() {
		return "", fmt.Errorf("refusing %s: it is not a directory", project)
	}
	if resolved == "/" {
		return "", errors.New("refusing /: _the-agent would be able to read every file on this machine")
	}
	home := host.Home
	if resolved == home {
		return "", fmt.Errorf("refusing %s: it is your home folder, so _the-agent could read all of it; run setup in a project folder", resolved)
	}
	if strings.HasPrefix(home, resolved+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing %s: it contains your home folder, so _the-agent could read all of it; run setup in a project folder", resolved)
	}
	return resolved, nil
}

type machine struct {
	host     Host
	platform platform
	dry_run  bool
}

func (current *machine) probe(arguments ...string) (string, bool) {
	output, error := current.host.Shell.Run(command(arguments...))
	return output, error == nil
}

func (current *machine) print(format string, arguments ...any) {
	fmt.Fprintf(current.host.Output, format, arguments...)
}
