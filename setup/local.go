package setup

import (
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

func Local(output io.Writer) (Host, error) {
	account, error := invoker()
	if error != nil {
		return Host{}, error
	}
	directory, error := os.Getwd()
	if error != nil {
		return Host{}, error
	}
	binary, error := Binary()
	if error != nil {
		return Host{}, error
	}
	return Host{
		System:    runtime.GOOS,
		Invoker:   account.Username,
		Home:      account.HomeDir,
		Directory: directory,
		Binary:    binary,
		Root:      os.Geteuid() == 0,
		Shell:     exec_shell{},
		Output:    output,
	}, nil
}

func invoker() (*user.User, error) {
	if name := os.Getenv("SUDO_USER"); name != "" && os.Geteuid() == 0 {
		return user.Lookup(name)
	}
	return user.Current()
}

type exec_shell struct{}

func (exec_shell) Run(command Command) (string, error) {
	process := exec.Command(command.Args[0], command.Args[1:]...)
	if command.Input != "" {
		process.Stdin = strings.NewReader(command.Input)
	}
	output, error := process.CombinedOutput()
	return string(output), error
}

func Home() string {
	target, error := platform_for(runtime.GOOS)
	if error != nil {
		return ""
	}
	return target.home()
}

func Binary() (string, error) {
	binary, error := os.Executable()
	if error != nil {
		return "", error
	}
	return filepath.EvalSymlinks(binary)
}

func Ready(project string, binary string) error {
	target, error := platform_for(runtime.GOOS)
	if error != nil {
		return error
	}
	current := &machine{host: Host{System: runtime.GOOS, Binary: binary, Shell: exec_shell{}, Output: io.Discard}, platform: target}
	return current.check(project)
}
