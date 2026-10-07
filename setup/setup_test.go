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
• caches /var/the-agent/{gocache,gomodcache,tmp,clones} (dry run)
    mkdir -p /var/the-agent/gocache /var/the-agent/gomodcache /var/the-agent/tmp /var/the-agent/clones
    chown -R _the-agent:_the-agent /var/the-agent
    chmod 0700 /var/the-agent
• ACL read on HOME/Documents/repos/app (inherit) (dry run)
    chmod -R +a '_the-agent allow read,execute,readattr,readextattr,readsecurity,file_inherit,directory_inherit' HOME/Documents/repos/app
    tee -a /var/the-agent/projects <<< HOME/Documents/repos/app
• search on HOME, HOME/Documents, HOME/Documents/repos (dry run)
    chmod +a '_the-agent allow search' HOME
    chmod +a '_the-agent allow search' HOME/Documents
    chmod +a '_the-agent allow search' HOME/Documents/repos
• check (dry run)
    sudo -n -u _the-agent ls HOME/Documents/repos/app
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
	current.respond("test -f /etc/sudoers.d/the-agent", "")
	for _, name := range []string{"gocache", "gomodcache", "tmp", "clones"} {
		current.respond("test -d /var/the-agent/"+name, "")
	}
	current.respond("cat /var/the-agent/projects", "/Users/nico/other\n")
	for _, directory := range []string{"HOME", "HOME/Documents"} {
		current.respond("ls -lde "+directory, "drwxr-xr-x+ 3 nico staff 96 "+directory+"\n 0: user:_the-agent allow search\n")
	}
	return current
}

func TestDryRun_LaterProjectPrintsOnlyTheProjectCommands(t *testing.T) {
	current := darwin_later_project(t)

	if error := current.run(t, "--dry-run"); error != nil {
		t.Fatal(error)
	}

	current.want_output(t, `✓ user _the-agent (exists)
✓ /etc/sudoers.d/the-agent (exists)
✓ caches /var/the-agent/{gocache,gomodcache,tmp,clones} (exists)
• ACL read on HOME/Documents/repos/app (inherit) (dry run)
    chmod -R +a '_the-agent allow read,execute,readattr,readextattr,readsecurity,file_inherit,directory_inherit' HOME/Documents/repos/app
    tee -a /var/the-agent/projects <<< HOME/Documents/repos/app
• search on HOME, HOME/Documents, HOME/Documents/repos (dry run)
    chmod +a '_the-agent allow search' HOME/Documents/repos
• check (dry run)
    sudo -n -u _the-agent ls HOME/Documents/repos/app
`)
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
• caches /var/lib/the-agent/{gocache,gomodcache,tmp,clones} (dry run)
    mkdir -p /var/lib/the-agent/gocache /var/lib/the-agent/gomodcache /var/lib/the-agent/tmp /var/lib/the-agent/clones
    chown -R _the-agent:_the-agent /var/lib/the-agent
    chmod 0700 /var/lib/the-agent
• ACL read on HOME/Documents/repos/app (inherit) (dry run)
    setfacl -R -m u:_the-agent:rX HOME/Documents/repos/app
    find HOME/Documents/repos/app -type d -exec setfacl -m d:u:_the-agent:rX '{}' +
    tee -a /var/lib/the-agent/projects <<< HOME/Documents/repos/app
• search on HOME, HOME/Documents, HOME/Documents/repos (dry run)
    setfacl -m u:_the-agent:x HOME
    setfacl -m u:_the-agent:x HOME/Documents
    setfacl -m u:_the-agent:x HOME/Documents/repos
• check (dry run)
    sudo -n -u _the-agent ls HOME/Documents/repos/app
`)
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
	current.respond("sudo -n -u _the-agent ls HOME/Documents/repos/app", "go.mod\n")
	return current
}

func TestSetup_OnASetUpProjectReportsExistsAndChangesNothing(t *testing.T) {
	current := darwin_set_up_project(t)

	if error := current.run(t); error != nil {
		t.Fatal(error)
	}

	current.want_output(t, `✓ user _the-agent (exists)
✓ /etc/sudoers.d/the-agent (exists)
✓ caches /var/the-agent/{gocache,gomodcache,tmp,clones} (exists)
✓ ACL read on HOME/Documents/repos/app (inherit) (exists)
✓ search on HOME, HOME/Documents, HOME/Documents/repos (exists)
✓ check: sudo -n -u _the-agent ls HOME/Documents/repos/app → ready
`)
	for _, line := range current.shell.ran {
		if !strings.HasPrefix(line, "id ") && !strings.HasPrefix(line, "test ") && !strings.HasPrefix(line, "cat ") && !strings.HasPrefix(line, "ls ") && !strings.HasPrefix(line, "sudo -n -u _the-agent ls ") {
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

	current.want_output(t, "✓ check: sudo -n -u _the-agent ls HOME/Documents/repos/app → ready\n")
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

	current.want_output(t, `• ACL read on HOME/Documents/repos/app (dry run)
    chmod -R -a '_the-agent allow read,execute,readattr,readextattr,readsecurity,file_inherit,directory_inherit' HOME/Documents/repos/app
    tee /var/the-agent/projects <<< HOME/Documents/repos/site
`)
}

func TestUninstall_LastProjectRemovesItsParentsSearch(t *testing.T) {
	current := darwin_set_up_project(t)
	current.respond("cat /var/the-agent/projects", "HOME/Documents/repos/app\n")

	if error := current.run(t, "--uninstall", "--dry-run"); error != nil {
		t.Fatal(error)
	}

	current.want_output(t, `• ACL read on HOME/Documents/repos/app (dry run)
    chmod -R -a '_the-agent allow read,execute,readattr,readextattr,readsecurity,file_inherit,directory_inherit' HOME/Documents/repos/app
    rm -f /var/the-agent/projects
• search on HOME, HOME/Documents, HOME/Documents/repos (dry run)
    chmod -a '_the-agent allow search' HOME
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

	current.want_output(t, `• ACL read on HOME/Documents/repos/app (dry run)
    chmod -R -a '_the-agent allow read,execute,readattr,readextattr,readsecurity,file_inherit,directory_inherit' HOME/Documents/repos/app
• search on HOME, HOME/Documents, HOME/Documents/repos (dry run)
    chmod -a '_the-agent allow search' HOME
    chmod -a '_the-agent allow search' HOME/Documents
    chmod -a '_the-agent allow search' HOME/Documents/repos
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

	current.want_output(t, `✓ home /var/the-agent (absent)
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
