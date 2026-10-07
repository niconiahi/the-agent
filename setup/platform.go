package setup

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const DARWIN_HOME = "/var/the-agent"
const LINUX_HOME = "/var/lib/the-agent"
const DARWIN_READ = USER + " allow read,execute,readattr,readextattr,readsecurity,file_inherit,directory_inherit"
const DARWIN_SEARCH = USER + " allow search"
const DARWIN_FULL = " allow list,add_file,search,delete,add_subdirectory,delete_child,readattr,writeattr,readextattr,writeextattr,readsecurity,writesecurity,chown,file_inherit,directory_inherit"
const DARWIN_FIRST_ID = 400
const DARWIN_LAST_ID = 499

var ERROR_NO_FREE_ID = fmt.Errorf("no free user and group id between %d and %d for %s", DARWIN_FIRST_ID, DARWIN_LAST_ID, USER)

type platform interface {
	home() string
	create_user(current *machine) ([]Command, error)
	delete_user() []Command
	has_read(current *machine, project string) bool
	grant_read(project string) []Command
	revoke_read(project string) []Command
	has_search(current *machine, directory string) bool
	grant_search(directory string) Command
	revoke_search(directory string) Command
	has_full(current *machine, directory string, invoker string) bool
	grant_full(invoker string, directories []string) Command
	diagnose(project string, output string) error
}

func platform_for(system string) (platform, error) {
	switch system {
	case SYSTEM_DARWIN:
		return darwin{}, nil
	case SYSTEM_LINUX:
		return linux{}, nil
	}
	return nil, ERROR_UNSUPPORTED_SYSTEM
}

func diagnose_common(project string, output string) error {
	if strings.Contains(output, "password is required") || strings.Contains(output, "unknown user") || strings.Contains(output, "not allowed") {
		return fmt.Errorf("sudo -n -u %s is not allowed yet (%s): run sudo the-agent setup", USER, strings.TrimSpace(output))
	}
	return fmt.Errorf("%s cannot read %s (%s): run sudo the-agent setup %s", USER, project, strings.TrimSpace(output), project)
}

type darwin struct{}

func (darwin) home() string {
	return DARWIN_HOME
}

func (darwin) create_user(current *machine) ([]Command, error) {
	users, users_found := current.probe("dscl", ".", "-list", "/Users", "UniqueID")
	groups, groups_found := current.probe("dscl", ".", "-list", "/Groups", "PrimaryGroupID")
	if !users_found || !groups_found {
		return nil, errors.New("cannot list users and groups with dscl")
	}
	used := map[int]bool{}
	for _, line := range strings.Split(users+"\n"+groups, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if id, error := strconv.Atoi(fields[len(fields)-1]); error == nil {
			used[id] = true
		}
	}
	for id := DARWIN_FIRST_ID; id <= DARWIN_LAST_ID; id++ {
		if used[id] {
			continue
		}
		number := strconv.Itoa(id)
		group := "/Groups/" + USER
		user := "/Users/" + USER
		return []Command{
			command("dscl", ".", "-create", group),
			command("dscl", ".", "-create", group, "PrimaryGroupID", number),
			command("dscl", ".", "-create", user),
			command("dscl", ".", "-create", user, "UniqueID", number),
			command("dscl", ".", "-create", user, "PrimaryGroupID", number),
			command("dscl", ".", "-create", user, "UserShell", "/usr/bin/false"),
			command("dscl", ".", "-create", user, "NFSHomeDirectory", DARWIN_HOME),
			command("dscl", ".", "-create", user, "Password", "*"),
			command("dscl", ".", "-create", user, "IsHidden", "1"),
		}, nil
	}
	return nil, ERROR_NO_FREE_ID
}

func (darwin) delete_user() []Command {
	return []Command{
		command("dscl", ".", "-delete", "/Users/"+USER),
		command("dscl", ".", "-delete", "/Groups/"+USER),
	}
}

func (darwin) has_read(current *machine, project string) bool {
	output, found := current.probe("ls", "-lde", project)
	return found && strings.Contains(output, "user:"+USER+" allow list,search")
}

func (darwin) grant_read(project string) []Command {
	return []Command{command("chmod", "-R", "+a", DARWIN_READ, project)}
}

func (darwin) revoke_read(project string) []Command {
	return []Command{command("chmod", "-R", "-a", DARWIN_READ, project)}
}

func (darwin) has_search(current *machine, directory string) bool {
	output, found := current.probe("ls", "-lde", directory)
	return found && strings.Contains(output, "user:"+USER+" allow search")
}

func (darwin) grant_search(directory string) Command {
	return command("chmod", "+a", DARWIN_SEARCH, directory)
}

func (darwin) revoke_search(directory string) Command {
	return command("chmod", "-a", DARWIN_SEARCH, directory)
}

func (darwin) has_full(current *machine, directory string, invoker string) bool {
	output, found := current.probe("ls", "-lde", directory)
	lines := strings.Split(output, "\n")
	fields := strings.Fields(lines[0])
	return found && len(fields) > 2 && fields[2] == USER && strings.Contains(output, "user:"+invoker+DARWIN_FULL)
}

func (darwin) grant_full(invoker string, directories []string) Command {
	return command(append([]string{"chmod", "+a", invoker + DARWIN_FULL}, directories...)...)
}

func (darwin) diagnose(project string, output string) error {
	if strings.Contains(output, "Operation not permitted") {
		return fmt.Errorf("macOS privacy protection (TCC) stops %s from reading %s even though the ACLs are in place. Open System Settings → Privacy & Security → Full Disk Access, allow the app you run Neovim in (your terminal), then run the-agent setup --check again", USER, project)
	}
	return diagnose_common(project, output)
}

type linux struct{}

func (linux) home() string {
	return LINUX_HOME
}

func (linux) create_user(current *machine) ([]Command, error) {
	return []Command{
		command("useradd", "--system", "--user-group", "--no-create-home", "--home-dir", LINUX_HOME, "--shell", "/usr/bin/false", USER),
	}, nil
}

func (linux) delete_user() []Command {
	return []Command{command("userdel", USER)}
}

func (linux) has_read(current *machine, project string) bool {
	output, found := current.probe("getfacl", "-cp", project)
	return found && strings.Contains(output, "user:"+USER+":r-x")
}

func (linux) grant_read(project string) []Command {
	return []Command{
		command("setfacl", "-R", "-m", "u:"+USER+":rX", project),
		command("find", project, "-type", "d", "-exec", "setfacl", "-m", "d:u:"+USER+":rX", "{}", "+"),
	}
}

func (linux) revoke_read(project string) []Command {
	return []Command{
		command("setfacl", "-R", "-x", "u:"+USER, project),
		command("find", project, "-type", "d", "-exec", "setfacl", "-x", "d:u:"+USER, "{}", "+"),
	}
}

func (linux) has_search(current *machine, directory string) bool {
	output, found := current.probe("getfacl", "-cp", directory)
	return found && (strings.Contains(output, "user:"+USER+":--x") || strings.Contains(output, "user:"+USER+":r-x"))
}

func (linux) grant_search(directory string) Command {
	return command("setfacl", "-m", "u:"+USER+":x", directory)
}

func (linux) revoke_search(directory string) Command {
	return command("setfacl", "-x", "u:"+USER, directory)
}

func (linux) has_full(current *machine, directory string, invoker string) bool {
	output, found := current.probe("getfacl", "-p", directory)
	lines := strings.Split(output, "\n")
	return found && contains(lines, "# owner: "+USER) && contains(lines, "user:"+invoker+":rwx") && contains(lines, "default:user:"+invoker+":rwx") && contains(lines, "mask::rwx") && contains(lines, "default:mask::rwx")
}

func (linux) grant_full(invoker string, directories []string) Command {
	entry := "u:" + invoker + ":rwx"
	return command(append([]string{"setfacl", "-m", entry + ",d:" + entry + ",m::rwx,d:m::rwx"}, directories...)...)
}

func (linux) diagnose(project string, output string) error {
	return diagnose_common(project, output)
}
