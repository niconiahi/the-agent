package setup

import (
	"fmt"
	"path/filepath"
	"strings"
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
	user, error := current.user_step()
	if error != nil {
		return error
	}
	steps := []step{
		user,
		current.sudoers_step(),
		current.caches_step(),
		current.read_step(project),
		current.search_step(parents(current.host.Home, project)),
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
	_, exists := current.probe("test", "-f", SUDOERS)
	line := fmt.Sprintf("%s ALL=(%s) NOPASSWD: ALL\n", current.host.Invoker, USER)
	return step{
		label: SUDOERS,
		done:  exists,
		commands: []Command{
			{Args: []string{"tee", SUDOERS_DRAFT}, Input: line},
			command("chmod", "0440", SUDOERS_DRAFT),
			command("visudo", "-cf", SUDOERS_DRAFT),
			command("mv", SUDOERS_DRAFT, SUDOERS),
		},
		on_failure: []Command{command("rm", "-f", SUDOERS_DRAFT)},
	}
}

func (current *machine) caches_step() step {
	home := current.platform.home()
	directories := make([]string, len(CACHES))
	for index, name := range CACHES {
		directories[index] = filepath.Join(home, name)
	}
	exists := true
	for _, directory := range directories {
		if _, found := current.probe("test", "-d", directory); !found {
			exists = false
		}
	}
	return step{
		label: fmt.Sprintf("caches %s/{%s}", home, strings.Join(CACHES, ",")),
		done:  exists,
		commands: []Command{
			command(append([]string{"mkdir", "-p"}, directories...)...),
			command("chown", "-R", USER+":"+USER, home),
			command("chmod", "0700", home),
		},
	}
}

func (current *machine) read_step(project string) step {
	granted := current.platform.has_read(current, project)
	registered := contains(current.projects(), project)
	each := step{label: fmt.Sprintf("ACL read on %s (inherit)", project), done: granted && registered}
	if !granted {
		each.commands = append(each.commands, current.platform.grant_read(project)...)
	}
	if !registered {
		each.commands = append(each.commands, Command{Args: []string{"tee", "-a", current.registry()}, Input: project + "\n"})
	}
	return each
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

func (current *machine) check(project string) error {
	probe := command("sudo", "-n", "-u", USER, "ls", project)
	if current.dry_run {
		current.print("• check (dry run)\n    %s\n", probe)
		return nil
	}
	output, error := current.host.Shell.Run(probe)
	if error == nil {
		current.print("✓ check: %s → ready\n", probe)
		return nil
	}
	return current.platform.diagnose(project, output)
}

func (current *machine) uninstall(project string) error {
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
	var shared []string
	for _, other := range others {
		shared = append(shared, parents(current.host.Home, other)...)
	}
	var directories []string
	for _, directory := range parents(current.host.Home, project) {
		if !contains(shared, directory) {
			directories = append(directories, directory)
		}
	}
	steps := []step{read}
	if len(directories) > 0 {
		steps = append(steps, current.revoke_search_step(directories))
	}
	return current.apply(steps, UNINSTALLED)
}

func (current *machine) uninstall_all() error {
	var steps []step
	var directories []string
	for _, project := range current.projects() {
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
	return Command{Args: []string{"tee", current.registry()}, Input: strings.Join(projects, "\n") + "\n"}
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
