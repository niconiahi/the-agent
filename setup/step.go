package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/niconiahi/the-agent/layout"
)

type step struct {
	label      string
	done       bool
	commands   []Command
	on_failure []Command
}

type outcome struct {
	done    string
	applied string
}

var INSTALLED = outcome{done: "exists", applied: "created"}
var UNINSTALLED = outcome{done: "absent", applied: "removed"}

func (current *machine) apply(steps []step, words outcome) error {
	for _, each := range steps {
		if each.done {
			current.print("✓ %s (%s)\n", each.label, words.done)
			continue
		}
		if current.dry_run {
			current.print("• %s (dry run)\n", each.label)
			for _, pending := range each.commands {
				current.print("    %s\n", pending)
			}
			continue
		}
		for _, pending := range each.commands {
			output, error := current.host.Shell.Run(pending)
			if error == nil {
				continue
			}
			for _, cleanup := range each.on_failure {
				current.host.Shell.Run(cleanup)
			}
			return fmt.Errorf("%s: %s failed (%v)\n%s", each.label, pending, error, strings.TrimSpace(output))
		}
		current.print("✓ %s (%s)\n", each.label, words.applied)
	}
	return nil
}

func (current *machine) install(project string) error {
	if error := current.refuse_links(project); error != nil {
		return error
	}
	user, error := current.user_step()
	if error != nil {
		return error
	}
	steps := []step{
		user,
		current.sudoers_step(),
		current.caches_step(),
		current.read_step(project),
		current.owned_step(project),
		current.search_step(parents(current.host.Home, project)),
		current.binary_step(project),
	}
	if error := current.apply(steps, INSTALLED); error != nil {
		return error
	}
	return current.check(project)
}

func (current *machine) user_step() (step, error) {
	_, exists := current.probe("id", "-u", USER)
	each := step{label: "user " + USER, done: exists}
	if exists {
		return each, nil
	}
	commands, error := current.platform.create_user(current)
	each.commands = commands
	return each, error
}

func (current *machine) sudoers_step() step {
	line := fmt.Sprintf("%s ALL=(%s) NOPASSWD: ALL", current.host.Invoker, USER)
	content, readable := current.probe("cat", SUDOERS)
	lines := strings.Split(strings.TrimSpace(content), "\n")
	each := step{label: SUDOERS, done: contains(lines, line)}
	if !readable {
		_, each.done = current.probe("test", "-f", SUDOERS)
		content = ""
	}
	if each.done {
		return each
	}
	if content = strings.TrimSpace(content); content != "" {
		content += "\n"
	}
	each.commands = []Command{
		{Arguments: []string{"tee", SUDOERS_DRAFT}, Input: content + line + "\n"},
		command("chmod", "0440", SUDOERS_DRAFT),
		command("visudo", "-cf", SUDOERS_DRAFT),
		command("mv", SUDOERS_DRAFT, SUDOERS),
	}
	each.on_failure = []Command{command("rm", "-f", SUDOERS_DRAFT)}
	return each
}

type listing struct {
	mode  string
	owner string
}

func (current *machine) list(path string) (listing, bool) {
	output, found := current.probe("ls", "-ld", path)
	fields := strings.Fields(output)
	if !found || len(fields) < 3 || len(fields[0]) < 10 {
		return listing{}, false
	}
	return listing{mode: fields[0][:10], owner: fields[2]}, true
}

func (current *machine) caches_step() step {
	home := current.platform.home()
	directories := make([]string, len(layout.CACHES))
	for index, name := range layout.CACHES {
		directories[index] = filepath.Join(home, name)
	}
	each := step{
		label: fmt.Sprintf("home %s owned by root, caches {%s} owned by %s", home, strings.Join(layout.CACHES, ","), USER),
		done:  true,
		commands: []Command{
			command("mkdir", "-p", home),
			command("chown", "0:0", home),
			command("chmod", "0755", home),
		},
	}
	if found, exists := current.list(home); !exists || found.owner != "root" || found.mode != "drwxr-xr-x" {
		each.done = false
	}
	for _, directory := range directories {
		found, exists := current.list(directory)
		if !exists || found.owner != USER || found.mode[0] != 'd' {
			each.done = false
		}
		if exists && found.mode[0] != 'd' {
			each.commands = append(each.commands, command("rm", "-f", directory))
		}
	}
	if found, exists := current.list(current.registry()); exists && (found.owner != "root" || found.mode[0] != '-') {
		each.done = false
		each.commands = append(each.commands, command("rm", "-f", current.registry()))
		if projects := current.projects(); len(projects) > 0 {
			each.commands = append(each.commands, current.rewrite_registry(projects))
		}
	}
	each.commands = append(each.commands,
		command(append([]string{"mkdir", "-p"}, directories...)...),
		command(append([]string{"chown", USER + ":" + USER}, directories...)...),
		command(append([]string{"chmod", "0700"}, directories...)...),
	)
	return each
}

func (current *machine) read_step(project string) step {
	granted := current.platform.has_read(current, project)
	registered := contains(current.projects(), project)
	each := step{label: fmt.Sprintf("ACL read on %s (inherit)", project), done: granted && registered}
	if !granted {
		each.commands = append(each.commands, current.platform.grant_read(project)...)
	}
	if !registered {
		each.commands = append(each.commands, Command{Arguments: []string{"tee", "-a", current.registry()}, Input: project + "\n"})
	}
	return each
}

func owned(project string) []string {
	directories := make([]string, len(layout.OWNED))
	for index, name := range layout.OWNED {
		directories[index] = filepath.Join(layout.Folder(project), name)
	}
	return directories
}

func (current *machine) refuse_links(project string) error {
	for _, path := range append([]string{project, layout.Folder(project)}, owned(project)...) {
		if _, link := current.probe("test", "-L", path); link {
			return fmt.Errorf("refusing %s: %s is a symbolic link", project, path)
		}
		if _, exists := current.probe("test", "-e", path); !exists {
			continue
		}
		if _, folder := current.probe("test", "-d", path); !folder {
			return fmt.Errorf("refusing %s: %s is not a folder", project, path)
		}
	}
	return nil
}

func (current *machine) owned_step(project string) step {
	invoker := current.host.Invoker
	folder := layout.Folder(project)
	directories := owned(project)
	each := step{
		label: fmt.Sprintf("%s/{%s} owned by %s, full control for %s", folder, strings.Join(layout.OWNED, ","), USER, invoker),
		done:  true,
	}
	for _, directory := range directories {
		if !current.platform.has_full(current, directory, invoker) {
			each.done = false
		}
	}
	if each.done {
		return each
	}
	if _, found := current.probe("test", "-d", folder); !found {
		each.commands = append(each.commands, command("mkdir", "-p", folder), command("chown", invoker+":", folder))
	}
	each.commands = append(each.commands,
		command(append([]string{"mkdir", "-p"}, directories...)...),
		command(append([]string{"chown", USER + ":" + USER}, directories...)...),
		command(append([]string{"chmod", "0700"}, directories...)...),
		current.platform.grant_full(invoker, directories),
	)
	return each
}

func (current *machine) binary_step(project string) step {
	binary := current.host.Binary
	each := step{label: "search on the folders holding " + binary, done: true}
	project_directories := parents(current.host.Home, project)
	var directories []string
	for _, directory := range parents(current.host.Home, binary) {
		if contains(project_directories, directory) || searchable(directory) || current.platform.has_search(current, directory) {
			continue
		}
		directories = append(directories, directory)
		each.done = false
		each.commands = append(each.commands, current.platform.grant_search(directory))
	}
	if len(directories) > 0 {
		each.label = fmt.Sprintf("search on %s for %s", strings.Join(directories, ", "), binary)
	}
	return each
}

func searchable(directory string) bool {
	info, error := os.Stat(directory)
	return error == nil && info.Mode().Perm()&0o001 != 0
}

func (current *machine) search_step(directories []string) step {
	each := step{label: "search on " + strings.Join(directories, ", "), done: true}
	for _, directory := range directories {
		if !current.platform.has_search(current, directory) {
			each.done = false
			each.commands = append(each.commands, current.platform.grant_search(directory))
		}
	}
	return each
}

type probe struct {
	command  Command
	diagnose func(output string) error
}

func (current *machine) probes(project string) []probe {
	as_user := func(arguments ...string) Command {
		return command(append([]string{"sudo", "-n", "-u", USER}, arguments...)...)
	}
	probes := []probe{{
		command: as_user("ls", project),
		diagnose: func(output string) error {
			return current.platform.diagnose(project, output)
		},
	}, {
		command: as_user("test", "-x", current.host.Binary),
		diagnose: func(string) error {
			return fmt.Errorf("%s cannot run %s: run sudo the-agent setup %s", USER, current.host.Binary, project)
		},
	}}
	for _, directory := range owned(project) {
		probes = append(probes, probe{
			command: as_user("test", "-w", directory),
			diagnose: func(string) error {
				return fmt.Errorf("%s cannot write %s: run sudo the-agent setup %s", USER, directory, project)
			},
		})
	}
	return probes
}

func (current *machine) check(project string) error {
	probes := current.probes(project)
	if current.dry_run {
		current.print("• check (dry run)\n")
		for _, each := range probes {
			current.print("    %s\n", each.command)
		}
		return nil
	}
	for _, each := range probes {
		output, error := current.host.Shell.Run(each.command)
		if error != nil {
			return each.diagnose(output)
		}
		current.print("✓ check: %s\n", each.command)
	}
	current.print("ready\n")
	return nil
}

func (current *machine) uninstall(project string) error {
	if error := current.refuse_links(project); error != nil {
		return error
	}
	others := without(current.projects(), project)
	granted := current.platform.has_read(current, project)
	registered := contains(current.projects(), project)
	read := step{label: fmt.Sprintf("ACL read on %s", project), done: !granted && !registered}
	if granted {
		read.commands = append(read.commands, current.platform.revoke_read(project)...)
	}
	if registered {
		read.commands = append(read.commands, current.rewrite_registry(others))
	}
	shared := parents(current.host.Home, current.host.Binary)
	for _, other := range others {
		shared = append(shared, parents(current.host.Home, other)...)
	}
	var directories []string
	for _, directory := range parents(current.host.Home, project) {
		if !contains(shared, directory) {
			directories = append(directories, directory)
		}
	}
	steps := []step{current.remove_owned_step(project), read}
	if len(directories) > 0 {
		steps = append(steps, current.revoke_search_step(directories))
	}
	return current.apply(steps, UNINSTALLED)
}

func (current *machine) uninstall_all() error {
	var steps []step
	var directories []string
	for _, project := range current.projects() {
		if error := current.refuse_links(project); error != nil {
			return error
		}
		steps = append(steps, current.remove_owned_step(project))
		read := step{label: fmt.Sprintf("ACL read on %s", project), done: !current.platform.has_read(current, project)}
		if !read.done {
			read.commands = current.platform.revoke_read(project)
		}
		steps = append(steps, read)
		for _, directory := range parents(current.host.Home, project) {
			if !contains(directories, directory) {
				directories = append(directories, directory)
			}
		}
	}
	for _, directory := range parents(current.host.Home, current.host.Binary) {
		if !contains(directories, directory) {
			directories = append(directories, directory)
		}
	}
	if len(directories) > 0 {
		steps = append(steps, current.revoke_search_step(directories))
	}
	home := current.platform.home()
	_, home_exists := current.probe("test", "-d", home)
	_, sudoers_exists := current.probe("test", "-f", SUDOERS)
	_, user_exists := current.probe("id", "-u", USER)
	steps = append(steps,
		step{label: "home " + home, done: !home_exists, commands: []Command{command("rm", "-rf", home)}},
		step{label: SUDOERS, done: !sudoers_exists, commands: []Command{command("rm", "-f", SUDOERS)}},
		step{label: "user " + USER, done: !user_exists, commands: current.platform.delete_user()},
	)
	return current.apply(steps, UNINSTALLED)
}

func (current *machine) remove_owned_step(project string) step {
	directories := owned(project)
	each := step{label: fmt.Sprintf("%s/{%s}", layout.Folder(project), strings.Join(layout.OWNED, ",")), done: true}
	for _, directory := range directories {
		if _, found := current.probe("test", "-e", directory); found {
			each.done = false
		}
	}
	each.commands = []Command{command(append([]string{"rm", "-rf"}, directories...)...)}
	return each
}

func (current *machine) revoke_search_step(directories []string) step {
	each := step{label: "search on " + strings.Join(directories, ", "), done: true}
	for _, directory := range directories {
		if current.platform.has_search(current, directory) {
			each.done = false
			each.commands = append(each.commands, current.platform.revoke_search(directory))
		}
	}
	return each
}

func (current *machine) registry() string {
	return filepath.Join(current.platform.home(), "projects")
}

func (current *machine) projects() []string {
	output, found := current.probe("cat", current.registry())
	if !found {
		return nil
	}
	var projects []string
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); line != "" && !contains(projects, line) {
			projects = append(projects, line)
		}
	}
	return projects
}

func (current *machine) rewrite_registry(projects []string) Command {
	if len(projects) == 0 {
		return command("rm", "-f", current.registry())
	}
	return Command{Arguments: []string{"tee", current.registry()}, Input: strings.Join(projects, "\n") + "\n"}
}

func parents(home string, project string) []string {
	var directories []string
	inside_home := strings.HasPrefix(project, home+string(filepath.Separator))
	for directory := filepath.Dir(project); directory != "/" && directory != "."; directory = filepath.Dir(directory) {
		directories = append([]string{directory}, directories...)
		if inside_home && directory == home {
			break
		}
	}
	return directories
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func without(values []string, target string) []string {
	var kept []string
	for _, value := range values {
		if value != target {
			kept = append(kept, value)
		}
	}
	return kept
}
