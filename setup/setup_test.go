package setup_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/setup"
)

type fake_shell struct {
	responses map[string]string
	failures  map[string]string
	ran       []string
}

func (shell *fake_shell) Run(command setup.Command) (string, error) {
	line := command.String()
	shell.ran = append(shell.ran, line)
	if output, found := shell.failures[line]; found {
		return output, errors.New("exit status 1")
	}
	if output, found := shell.responses[line]; found {
		return output, nil
	}
	return "", errors.New("exit status 1")
}

type machine struct {
	home    string
	project string
	shell   *fake_shell
	output  *bytes.Buffer
	host    setup.Host
}

func new_machine(t *testing.T, system string) *machine {
	t.Helper()
	home, error := filepath.EvalSymlinks(t.TempDir())
	if error != nil {
		t.Fatal(error)
	}
	project := filepath.Join(home, "Documents", "repos", "app")
	if error := os.MkdirAll(project, 0o755); error != nil {
		t.Fatal(error)
	}
	binary := filepath.Join(home, "bin", "the-agent")
	if error := os.MkdirAll(filepath.Dir(binary), 0o700); error != nil {
		t.Fatal(error)
	}
	shell := &fake_shell{responses: map[string]string{}, failures: map[string]string{}}
	output := &bytes.Buffer{}
	return &machine{
		home:    home,
		project: project,
		shell:   shell,
		output:  output,
		host: setup.Host{
			System:    system,
			Invoker:   "nico",
			Home:      home,
			Directory: project,
			Binary:    binary,
			Root:      true,
			Shell:     shell,
			Output:    output,
		},
	}
}

func (current *machine) expand(text string) string {
	return strings.ReplaceAll(text, "HOME", current.home)
}

func (current *machine) respond(command string, output string) {
	current.shell.responses[current.expand(command)] = current.expand(output)
}

func (current *machine) fail(command string, output string) {
	current.shell.failures[current.expand(command)] = current.expand(output)
}

func (current *machine) run(t *testing.T, arguments ...string) error {
	t.Helper()
	return setup.Run(current.host, arguments)
}

func (current *machine) want_output(t *testing.T, want string) {
	t.Helper()
	if got := current.output.String(); got != current.expand(want) {
		t.Fatalf("output:\n%s\nwant:\n%s", got, current.expand(want))
	}
}

func (current *machine) ran(command string) bool {
	for _, line := range current.shell.ran {
		if line == current.expand(command) {
			return true
		}
	}
	return false
}

const DARWIN_UIDS = "root 0\n_oahd 441\nnico 501\n"
const DARWIN_GIDS = "wheel 0\n_spare 400\nstaff 20\n"

func darwin_first_project(t *testing.T) *machine {
	current := new_machine(t, "darwin")
	current.respond("dscl . -list /Users UniqueID", DARWIN_UIDS)
	current.respond("dscl . -list /Groups PrimaryGroupID", DARWIN_GIDS)
	return current
}

const DARWIN_FIRST_PROJECT = `• user _the-agent (dry run)
    dscl . -create /Groups/_the-agent
    dscl . -create /Groups/_the-agent PrimaryGroupID 401
    dscl . -create /Users/_the-agent
    dscl . -create /Users/_the-agent UniqueID 401
    dscl . -create /Users/_the-agent PrimaryGroupID 401
    dscl . -create /Users/_the-agent UserShell /usr/bin/false
    dscl . -create /Users/_the-agent NFSHomeDirectory /var/the-agent
    dscl . -create /Users/_the-agent Password '*'
    dscl . -create /Users/_the-agent IsHidden 1
• /etc/sudoers.d/the-agent (dry run)
    tee /etc/sudoers.d/the-agent.tmp <<< 'nico ALL=(_the-agent) NOPASSWD: ALL'
    chmod 0440 /etc/sudoers.d/the-agent.tmp
    visudo -cf /etc/sudoers.d/the-agent.tmp
    mv /etc/sudoers.d/the-agent.tmp /etc/sudoers.d/the-agent
• home /var/the-agent owned by root, caches {gocache,gomodcache} owned by _the-agent (dry run)
    mkdir -p /var/the-agent
    chown 0:0 /var/the-agent
    chmod 0755 /var/the-agent
    mkdir -p /var/the-agent/gocache /var/the-agent/gomodcache
    chown _the-agent:_the-agent /var/the-agent/gocache /var/the-agent/gomodcache
    chmod 0700 /var/the-agent/gocache /var/the-agent/gomodcache
• ACL read on HOME/Documents/repos/app (inherit) (dry run)
    chmod -R +a '_the-agent allow read,execute,readattr,readextattr,readsecurity,file_inherit,directory_inherit' HOME/Documents/repos/app
    tee -a /var/the-agent/projects <<< HOME/Documents/repos/app
` + DARWIN_CLONE + `• search on HOME, HOME/Documents, HOME/Documents/repos (dry run)
    chmod +a '_the-agent allow search' HOME
    chmod +a '_the-agent allow search' HOME/Documents
    chmod +a '_the-agent allow search' HOME/Documents/repos
• search on HOME/bin for HOME/bin/the-agent (dry run)
    chmod +a '_the-agent allow search' HOME/bin
` + DARWIN_CHECK

const DARWIN_CLONE = `• HOME/Documents/repos/app/.the-agent/{clone,tmp} owned by _the-agent, full control for nico (dry run)
    mkdir -p HOME/Documents/repos/app/.the-agent
    chown nico: HOME/Documents/repos/app/.the-agent
    mkdir -p HOME/Documents/repos/app/.the-agent/clone HOME/Documents/repos/app/.the-agent/tmp
    chown _the-agent:_the-agent HOME/Documents/repos/app/.the-agent/clone HOME/Documents/repos/app/.the-agent/tmp
    chmod 0700 HOME/Documents/repos/app/.the-agent/clone HOME/Documents/repos/app/.the-agent/tmp
    chmod +a 'nico allow list,add_file,search,delete,add_subdirectory,delete_child,readattr,writeattr,readextattr,writeextattr,readsecurity,writesecurity,chown,file_inherit,directory_inherit' HOME/Documents/repos/app/.the-agent/clone HOME/Documents/repos/app/.the-agent/tmp
`

const DARWIN_CHECK = `• check (dry run)
    sudo -n -u _the-agent ls HOME/Documents/repos/app
    sudo -n -u _the-agent test -x HOME/bin/the-agent
    sudo -n -u _the-agent test -w HOME/Documents/repos/app/.the-agent/clone
    sudo -n -u _the-agent test -w HOME/Documents/repos/app/.the-agent/tmp
`

func TestDryRun_FirstProjectPrintsEveryCommand(t *testing.T) {
	current := darwin_first_project(t)

	if error := current.run(t, "--dry-run"); error != nil {
		t.Fatal(error)
	}

	current.want_output(t, DARWIN_FIRST_PROJECT)
}

func darwin_later_project(t *testing.T) *machine {
	current := darwin_first_project(t)
	current.respond("id -u _the-agent", "401\n")
	current.respond("cat /etc/sudoers.d/the-agent", "nico ALL=(_the-agent) NOPASSWD: ALL\n")
	current.respond("test -f /etc/sudoers.d/the-agent", "")
	current.respond("ls -ld /var/the-agent", "drwxr-xr-x  4 root  wheel  128 Oct  7 10:00 /var/the-agent\n")
	for _, name := range []string{"gocache", "gomodcache"} {
		current.respond("ls -ld /var/the-agent/"+name, "drwx------  4 _the-agent  _the-agent  128 Oct  7 10:00 /var/the-agent/"+name+"\n")
	}
	current.respond("ls -ld /var/the-agent/projects", "-rw-r--r--  1 root  wheel  20 Oct  7 10:00 /var/the-agent/projects\n")
	current.respond("cat /var/the-agent/projects", "/Users/nico/other\n")
	for _, directory := range []string{"HOME", "HOME/Documents"} {
		current.respond("ls -lde "+directory, "drwxr-xr-x+ 3 nico staff 96 "+directory+"\n 0: user:_the-agent allow search\n")
	}
	current.respond("ls -lde HOME/bin", "drwx------+ 3 nico staff 96 bin\n 0: user:_the-agent allow search\n")
	current.respond("test -d HOME/Documents/repos/app/.the-agent", "")
	return current
}

func TestDryRun_LaterProjectPrintsOnlyTheProjectCommands(t *testing.T) {
	current := darwin_later_project(t)

	if error := current.run(t, "--dry-run"); error != nil {
		t.Fatal(error)
	}

	current.want_output(t, `✓ user _the-agent (exists)
✓ /etc/sudoers.d/the-agent (exists)
✓ home /var/the-agent owned by root, caches {gocache,gomodcache} owned by _the-agent (exists)
• ACL read on HOME/Documents/repos/app (inherit) (dry run)
    chmod -R +a '_the-agent allow read,execute,readattr,readextattr,readsecurity,file_inherit,directory_inherit' HOME/Documents/repos/app
    tee -a /var/the-agent/projects <<< HOME/Documents/repos/app
• HOME/Documents/repos/app/.the-agent/{clone,tmp} owned by _the-agent, full control for nico (dry run)
    mkdir -p HOME/Documents/repos/app/.the-agent/clone HOME/Documents/repos/app/.the-agent/tmp
    chown _the-agent:_the-agent HOME/Documents/repos/app/.the-agent/clone HOME/Documents/repos/app/.the-agent/tmp
    chmod 0700 HOME/Documents/repos/app/.the-agent/clone HOME/Documents/repos/app/.the-agent/tmp
    chmod +a 'nico allow list,add_file,search,delete,add_subdirectory,delete_child,readattr,writeattr,readextattr,writeextattr,readsecurity,writesecurity,chown,file_inherit,directory_inherit' HOME/Documents/repos/app/.the-agent/clone HOME/Documents/repos/app/.the-agent/tmp
• search on HOME, HOME/Documents, HOME/Documents/repos (dry run)
    chmod +a '_the-agent allow search' HOME/Documents/repos
✓ search on the folders holding HOME/bin/the-agent (exists)
`+DARWIN_CHECK)
}

func TestDryRun_RunsNoneOfThePrintedCommands(t *testing.T) {
	current := darwin_first_project(t)
	current.host.Root = false

	if error := current.run(t, "--dry-run"); error != nil {
		t.Fatal(error)
	}

	for _, line := range strings.Split(current.output.String(), "\n") {
		if printed, found := strings.CutPrefix(line, "    "); found && current.ran(printed) {
			t.Errorf("dry run ran %q", printed)
		}
	}
}

func TestDryRun_FirstProjectOnLinux(t *testing.T) {
	current := new_machine(t, "linux")
	current.host.Directory = current.home
	current.host.Root = false

	if error := current.run(t, "--dry-run", "Documents/repos/app"); error != nil {
		t.Fatal(error)
	}

	current.want_output(t, `• user _the-agent (dry run)
    useradd --system --user-group --no-create-home --home-dir /var/lib/the-agent --shell /usr/bin/false _the-agent
• /etc/sudoers.d/the-agent (dry run)
    tee /etc/sudoers.d/the-agent.tmp <<< 'nico ALL=(_the-agent) NOPASSWD: ALL'
    chmod 0440 /etc/sudoers.d/the-agent.tmp
    visudo -cf /etc/sudoers.d/the-agent.tmp
    mv /etc/sudoers.d/the-agent.tmp /etc/sudoers.d/the-agent
• home /var/lib/the-agent owned by root, caches {gocache,gomodcache} owned by _the-agent (dry run)
    mkdir -p /var/lib/the-agent
    chown 0:0 /var/lib/the-agent
    chmod 0755 /var/lib/the-agent
    mkdir -p /var/lib/the-agent/gocache /var/lib/the-agent/gomodcache
    chown _the-agent:_the-agent /var/lib/the-agent/gocache /var/lib/the-agent/gomodcache
    chmod 0700 /var/lib/the-agent/gocache /var/lib/the-agent/gomodcache
• ACL read on HOME/Documents/repos/app (inherit) (dry run)
    setfacl -R -m u:_the-agent:rX HOME/Documents/repos/app
    find HOME/Documents/repos/app -type d -exec setfacl -m d:u:_the-agent:rX '{}' +
    tee -a /var/lib/the-agent/projects <<< HOME/Documents/repos/app
• HOME/Documents/repos/app/.the-agent/{clone,tmp} owned by _the-agent, full control for nico (dry run)
    mkdir -p HOME/Documents/repos/app/.the-agent
    chown nico: HOME/Documents/repos/app/.the-agent
    mkdir -p HOME/Documents/repos/app/.the-agent/clone HOME/Documents/repos/app/.the-agent/tmp
    chown _the-agent:_the-agent HOME/Documents/repos/app/.the-agent/clone HOME/Documents/repos/app/.the-agent/tmp
    chmod 0700 HOME/Documents/repos/app/.the-agent/clone HOME/Documents/repos/app/.the-agent/tmp
    setfacl -m u:nico:rwx,d:u:nico:rwx,m::rwx,d:m::rwx HOME/Documents/repos/app/.the-agent/clone HOME/Documents/repos/app/.the-agent/tmp
• search on HOME, HOME/Documents, HOME/Documents/repos (dry run)
    setfacl -m u:_the-agent:x HOME
    setfacl -m u:_the-agent:x HOME/Documents
    setfacl -m u:_the-agent:x HOME/Documents/repos
• search on HOME/bin for HOME/bin/the-agent (dry run)
    setfacl -m u:_the-agent:x HOME/bin
`+DARWIN_CHECK)
}

func TestSetup_RefusesRootHomeAndFiles(t *testing.T) {
	current := darwin_first_project(t)
	file := filepath.Join(current.project, "main.go")
	if error := os.WriteFile(file, []byte("package main\n"), 0o644); error != nil {
		t.Fatal(error)
	}
	cases := []struct {
		target string
		want   string
	}{
		{"/", "refusing /: _the-agent would be able to read every file on this machine"},
		{current.home, "refusing " + current.home + ": it is your home folder"},
		{filepath.Dir(current.home), "refusing " + filepath.Dir(current.home) + ": it contains your home folder"},
		{file, "refusing " + file + ": it is not a directory"},
		{filepath.Join(current.project, "missing"), "it is not a directory"},
	}
	for _, each := range cases {
		for _, flags := range [][]string{{}, {"--dry-run"}, {"--uninstall"}} {
			current.shell.ran = nil
			current.output.Reset()
			error := current.run(t, append(flags, each.target)...)
			if error == nil || !strings.Contains(error.Error(), each.want) {
				t.Errorf("setup %v %s: got %v, want %q", flags, each.target, error, each.want)
			}
			if len(current.shell.ran) != 0 || current.output.Len() != 0 {
				t.Errorf("setup %v %s ran %v and printed %q", flags, each.target, current.shell.ran, current.output.String())
			}
		}
	}
}

func darwin_set_up_project(t *testing.T) *machine {
	current := darwin_later_project(t)
	current.respond("cat /var/the-agent/projects", "HOME/Documents/repos/app\nHOME/Documents/repos/site\n")
	current.respond("ls -lde HOME/Documents/repos", "drwxr-xr-x+ 3 nico staff 96 repos\n 0: user:_the-agent allow search\n")
	current.respond("ls -lde HOME/Documents/repos/app", "drwxr-xr-x+ 3 nico staff 96 app\n 0: user:_the-agent allow list,search,readattr,readextattr,readsecurity,file_inherit,directory_inherit\n")
	for _, name := range []string{"clone", "tmp"} {
		directory := "HOME/Documents/repos/app/.the-agent/" + name
		current.respond("ls -lde "+directory, "drwx------+ 2 _the-agent _the-agent 64 "+name+"\n 0: user:nico allow list,add_file,search,delete,add_subdirectory,delete_child,readattr,writeattr,readextattr,writeextattr,readsecurity,writesecurity,chown,file_inherit,directory_inherit\n")
		current.respond("test -e "+directory, "")
		current.respond("test -d "+directory, "")
		current.respond("sudo -n -u _the-agent test -w "+directory, "")
	}
	current.respond("sudo -n -u _the-agent ls HOME/Documents/repos/app", "go.mod\n")
	current.respond("sudo -n -u _the-agent test -x HOME/bin/the-agent", "")
	return current
}

const DARWIN_READY = `✓ check: sudo -n -u _the-agent ls HOME/Documents/repos/app
✓ check: sudo -n -u _the-agent test -x HOME/bin/the-agent
✓ check: sudo -n -u _the-agent test -w HOME/Documents/repos/app/.the-agent/clone
✓ check: sudo -n -u _the-agent test -w HOME/Documents/repos/app/.the-agent/tmp
ready
`

func TestSetup_OnASetUpProjectReportsExistsAndChangesNothing(t *testing.T) {
	current := darwin_set_up_project(t)

	if error := current.run(t); error != nil {
		t.Fatal(error)
	}

	current.want_output(t, `✓ user _the-agent (exists)
✓ /etc/sudoers.d/the-agent (exists)
✓ home /var/the-agent owned by root, caches {gocache,gomodcache} owned by _the-agent (exists)
✓ ACL read on HOME/Documents/repos/app (inherit) (exists)
✓ HOME/Documents/repos/app/.the-agent/{clone,tmp} owned by _the-agent, full control for nico (exists)
✓ search on HOME, HOME/Documents, HOME/Documents/repos (exists)
✓ search on the folders holding HOME/bin/the-agent (exists)
`+DARWIN_READY)
	for _, line := range current.shell.ran {
		if !strings.HasPrefix(line, "id ") && !strings.HasPrefix(line, "test ") && !strings.HasPrefix(line, "cat ") && !strings.HasPrefix(line, "ls ") && !strings.HasPrefix(line, "sudo -n -u _the-agent ls ") && !strings.HasPrefix(line, "sudo -n -u _the-agent test ") {
			t.Errorf("a set-up project ran %q", line)
		}
	}
}

func TestSetup_NeverInstallsAnInvalidSudoersFile(t *testing.T) {
	current := darwin_first_project(t)
	for _, line := range strings.Split(DARWIN_FIRST_PROJECT, "\n") {
		if printed, found := strings.CutPrefix(line, "    "); found {
			current.respond(printed, "")
		}
	}
	current.fail("visudo -cf /etc/sudoers.d/the-agent.tmp", "/etc/sudoers.d/the-agent.tmp:1: syntax error")
	current.respond("rm -f /etc/sudoers.d/the-agent.tmp", "")

	error := current.run(t)

	if error == nil || !strings.Contains(error.Error(), "syntax error") {
		t.Fatalf("got %v", error)
	}
	if current.ran("mv /etc/sudoers.d/the-agent.tmp /etc/sudoers.d/the-agent") {
		t.Fatal("installed a sudoers file visudo rejected")
	}
	if !current.ran("rm -f /etc/sudoers.d/the-agent.tmp") {
		t.Fatal("left the rejected draft behind")
	}
}

func TestCheck_ReportsReady(t *testing.T) {
	current := darwin_set_up_project(t)
	current.host.Root = false

	if error := current.run(t, "--check"); error != nil {
		t.Fatal(error)
	}

	current.want_output(t, DARWIN_READY)
}

func TestCheck_NamesTheTCCPermissionOnMacOS(t *testing.T) {
	current := darwin_set_up_project(t)
	current.host.Root = false
	current.fail("sudo -n -u _the-agent ls HOME/Documents/repos/app", "ls: HOME/Documents/repos/app: Operation not permitted\n")

	error := current.run(t, "--check")

	if error == nil || !strings.Contains(error.Error(), "System Settings → Privacy & Security → Full Disk Access") {
		t.Fatalf("got %v", error)
	}
}

func TestCheck_WithoutSetupSaysToRunIt(t *testing.T) {
	current := darwin_first_project(t)
	current.host.Root = false
	current.fail("sudo -n -u _the-agent ls HOME/Documents/repos/app", "sudo: unknown user _the-agent\n")

	error := current.run(t, "--check")

	if error == nil || !strings.Contains(error.Error(), "run sudo the-agent setup") {
		t.Fatalf("got %v", error)
	}
}

func TestUninstall_RemovesOneProjectAndKeepsSharedParents(t *testing.T) {
	current := darwin_set_up_project(t)

	if error := current.run(t, "--uninstall", "--dry-run"); error != nil {
		t.Fatal(error)
	}

	current.want_output(t, DARWIN_REMOVE_OWNED+`• ACL read on HOME/Documents/repos/app (dry run)
    chmod -R -a '_the-agent allow read,execute,readattr,readextattr,readsecurity,file_inherit,directory_inherit' HOME/Documents/repos/app
    tee /var/the-agent/projects <<< HOME/Documents/repos/site
`)
}

const DARWIN_REMOVE_OWNED = `• HOME/Documents/repos/app/.the-agent/{clone,tmp} (dry run)
    rm -rf HOME/Documents/repos/app/.the-agent/clone HOME/Documents/repos/app/.the-agent/tmp
`

func TestUninstall_OnAnUninstalledProjectReportsAbsent(t *testing.T) {
	current := darwin_first_project(t)
	current.respond("cat /var/the-agent/projects", "HOME/Documents/repos/site\n")

	if error := current.run(t, "--uninstall"); error != nil {
		t.Fatal(error)
	}

	current.want_output(t, `✓ HOME/Documents/repos/app/.the-agent/{clone,tmp} (absent)
✓ ACL read on HOME/Documents/repos/app (absent)
`)
}

func TestUninstall_LastProjectRemovesItsParentsSearchExceptTheBinarys(t *testing.T) {
	current := darwin_set_up_project(t)
	current.respond("cat /var/the-agent/projects", "HOME/Documents/repos/app\n")

	if error := current.run(t, "--uninstall", "--dry-run"); error != nil {
		t.Fatal(error)
	}

	current.want_output(t, DARWIN_REMOVE_OWNED+`• ACL read on HOME/Documents/repos/app (dry run)
    chmod -R -a '_the-agent allow read,execute,readattr,readextattr,readsecurity,file_inherit,directory_inherit' HOME/Documents/repos/app
    rm -f /var/the-agent/projects
• search on HOME/Documents, HOME/Documents/repos (dry run)
    chmod -a '_the-agent allow search' HOME/Documents
    chmod -a '_the-agent allow search' HOME/Documents/repos
`)
}

func TestUninstallAll_RemovesEverything(t *testing.T) {
	current := darwin_set_up_project(t)
	current.respond("cat /var/the-agent/projects", "HOME/Documents/repos/app\n")
	current.respond("test -d /var/the-agent", "")

	if error := current.run(t, "--uninstall", "--all", "--dry-run"); error != nil {
		t.Fatal(error)
	}

	current.want_output(t, DARWIN_REMOVE_OWNED+`• ACL read on HOME/Documents/repos/app (dry run)
    chmod -R -a '_the-agent allow read,execute,readattr,readextattr,readsecurity,file_inherit,directory_inherit' HOME/Documents/repos/app
• search on HOME, HOME/Documents, HOME/Documents/repos, HOME/bin (dry run)
    chmod -a '_the-agent allow search' HOME
    chmod -a '_the-agent allow search' HOME/Documents
    chmod -a '_the-agent allow search' HOME/Documents/repos
    chmod -a '_the-agent allow search' HOME/bin
• home /var/the-agent (dry run)
    rm -rf /var/the-agent
• /etc/sudoers.d/the-agent (dry run)
    rm -f /etc/sudoers.d/the-agent
• user _the-agent (dry run)
    dscl . -delete /Users/_the-agent
    dscl . -delete /Groups/_the-agent
`)
}

func TestUninstallAll_OnACleanMachineReportsAbsent(t *testing.T) {
	current := darwin_first_project(t)

	if error := current.run(t, "--uninstall", "--all"); error != nil {
		t.Fatal(error)
	}

	current.want_output(t, `✓ search on HOME, HOME/bin (absent)
✓ home /var/the-agent (absent)
✓ /etc/sudoers.d/the-agent (absent)
✓ user _the-agent (absent)
`)
}

func TestSetup_WithoutRootRefusesToChangeAnything(t *testing.T) {
	current := darwin_first_project(t)
	current.host.Root = false

	error := current.run(t)

	if error == nil || !strings.Contains(error.Error(), "sudo the-agent setup") {
		t.Fatalf("got %v", error)
	}
	if len(current.shell.ran) != 0 {
		t.Fatalf("ran %v", current.shell.ran)
	}
}

func TestCheck_WhenTheAgentCannotRunTheBinaryNamesSetup(t *testing.T) {
	current := darwin_set_up_project(t)
	current.host.Root = false
	current.fail("sudo -n -u _the-agent test -x HOME/bin/the-agent", "")

	error := current.run(t, "--check")

	if error == nil || error.Error() != current.expand("_the-agent cannot run HOME/bin/the-agent: run sudo the-agent setup HOME/Documents/repos/app") {
		t.Fatalf("got %v", error)
	}
}

func TestCheck_WhenTheAgentCannotWriteTheCloneNamesSetup(t *testing.T) {
	current := darwin_set_up_project(t)
	current.host.Root = false
	current.fail("sudo -n -u _the-agent test -w HOME/Documents/repos/app/.the-agent/clone", "")

	error := current.run(t, "--check")

	if error == nil || error.Error() != current.expand("_the-agent cannot write HOME/Documents/repos/app/.the-agent/clone: run sudo the-agent setup HOME/Documents/repos/app") {
		t.Fatalf("got %v", error)
	}
}

func TestSetup_TakesBackAHomeThatTheAgentOwned(t *testing.T) {
	current := darwin_set_up_project(t)
	current.respond("ls -ld /var/the-agent", "drwx------  4 _the-agent  _the-agent  128 Oct  7 10:00 /var/the-agent\n")
	current.respond("ls -ld /var/the-agent/projects", "lrwxr-xr-x  1 _the-agent  _the-agent  10 Oct  7 10:00 /var/the-agent/projects -> /tmp/fake\n")
	current.respond("ls -ld /var/the-agent/gocache", "lrwxr-xr-x  1 _the-agent  _the-agent  10 Oct  7 10:00 /var/the-agent/gocache -> /etc\n")

	if error := current.run(t, "--dry-run"); error != nil {
		t.Fatal(error)
	}

	want := `• home /var/the-agent owned by root, caches {gocache,gomodcache} owned by _the-agent (dry run)
    mkdir -p /var/the-agent
    chown 0:0 /var/the-agent
    chmod 0755 /var/the-agent
    rm -f /var/the-agent/gocache
    rm -f /var/the-agent/projects
    tee /var/the-agent/projects <<< 'HOME/Documents/repos/app
HOME/Documents/repos/site'
    mkdir -p /var/the-agent/gocache /var/the-agent/gomodcache
    chown _the-agent:_the-agent /var/the-agent/gocache /var/the-agent/gomodcache
    chmod 0700 /var/the-agent/gocache /var/the-agent/gomodcache
`
	if got := current.output.String(); !strings.Contains(got, current.expand(want)) {
		t.Fatalf("output:\n%s\nwant it to contain:\n%s", got, current.expand(want))
	}
}

func TestUninstall_RefusesATheAgentFolderThatIsASymbolicLink(t *testing.T) {
	current := darwin_set_up_project(t)
	current.respond("test -L HOME/Documents/repos/app/.the-agent", "")

	error := current.run(t, "--uninstall")

	if error == nil || error.Error() != current.expand("refusing HOME/Documents/repos/app: HOME/Documents/repos/app/.the-agent is a symbolic link") {
		t.Fatalf("got %v", error)
	}
	for _, line := range current.shell.ran {
		if strings.HasPrefix(line, "rm ") || strings.HasPrefix(line, "chmod ") || strings.HasPrefix(line, "tee ") {
			t.Errorf("a refused uninstall ran %q", line)
		}
	}
}

func TestUninstallAll_RefusesARegisteredProjectWhoseCloneIsNotAFolder(t *testing.T) {
	current := darwin_set_up_project(t)
	current.respond("cat /var/the-agent/projects", "HOME/Documents/repos/app\n")
	current.fail("test -d HOME/Documents/repos/app/.the-agent/clone", "")

	error := current.run(t, "--uninstall", "--all")

	if error == nil || error.Error() != current.expand("refusing HOME/Documents/repos/app: HOME/Documents/repos/app/.the-agent/clone is not a folder") {
		t.Fatalf("got %v", error)
	}
	for _, line := range current.shell.ran {
		if strings.HasPrefix(line, "rm ") || strings.HasPrefix(line, "chmod ") || strings.HasPrefix(line, "dscl ") {
			t.Errorf("a refused uninstall ran %q", line)
		}
	}
}

func TestSetup_AddsMyLineToASudoersFileThatLacksIt(t *testing.T) {
	current := darwin_set_up_project(t)
	current.respond("cat /etc/sudoers.d/the-agent", "ana ALL=(_the-agent) NOPASSWD: ALL\n")

	if error := current.run(t, "--dry-run"); error != nil {
		t.Fatal(error)
	}

	want := `• /etc/sudoers.d/the-agent (dry run)
    tee /etc/sudoers.d/the-agent.tmp <<< 'ana ALL=(_the-agent) NOPASSWD: ALL
nico ALL=(_the-agent) NOPASSWD: ALL'
    chmod 0440 /etc/sudoers.d/the-agent.tmp
    visudo -cf /etc/sudoers.d/the-agent.tmp
    mv /etc/sudoers.d/the-agent.tmp /etc/sudoers.d/the-agent
`
	if got := current.output.String(); !strings.Contains(got, want) {
		t.Fatalf("output:\n%s\nwant it to contain:\n%s", got, want)
	}
}

func TestDryRun_WithoutRootTrustsAnUnreadableSudoersFile(t *testing.T) {
	current := darwin_set_up_project(t)
	current.host.Root = false
	current.fail("cat /etc/sudoers.d/the-agent", "cat: /etc/sudoers.d/the-agent: Permission denied")
	current.respond("test -f /etc/sudoers.d/the-agent", "")

	if error := current.run(t, "--dry-run"); error != nil {
		t.Fatal(error)
	}

	if got := current.output.String(); !strings.Contains(got, "✓ /etc/sudoers.d/the-agent (exists)") {
		t.Fatalf("output:\n%s", got)
	}
}
