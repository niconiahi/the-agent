package tool

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type Runner func(invocation_context context.Context, directory string, script string) *exec.Cmd

// Clone is the project's persistent copy at <project>/.the-agent/clone, which
// _the-agent owns. Every step runs through Run (Sandbox.Command in production)
// with the project as the working directory, and hands its results back on
// stdout, so nothing here needs to read the clone as me.
type Clone struct {
	Project string
	Binary  string
	Run     Runner
}

type step_failure struct {
	stderr    string
	exit_code int
}

func (failure *step_failure) Error() string {
	return fmt.Sprintf("exit status %d: %s", failure.exit_code, strings.TrimSpace(failure.stderr))
}

var project_locks sync.Map

// lock serializes bash_write calls on one project, across tool instances.
func (clone Clone) lock() func() {
	value, _ := project_locks.LoadOrStore(clone.Project, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

func (clone Clone) Path() string {
	return filepath.Join(clone.Project, ".the-agent", "clone")
}

// sync runs "the-agent sync <project>" and then lists the clone, in one call.
func (clone Clone) sync(invocation_context context.Context) (map[string]string, error) {
	script := shell_quote(clone.Binary) + " sync " + shell_quote(clone.Project) + " || exit\n" + inside(clone.Path(), MANIFEST_SCRIPT)
	output, error := run_step(clone.Run(invocation_context, clone.Project, script), nil)
	if error != nil {
		return nil, fmt.Errorf("failed to sync the clone: %w", error)
	}
	return parse_manifest(output)
}

func (clone Clone) command(invocation_context context.Context, script string) *exec.Cmd {
	return clone.Run(invocation_context, clone.Project, inside(clone.Path(), script))
}

// changes lists the clone again and compares it with the listing from before
// the command, so files I save in the project meanwhile are left alone.
func (clone Clone) changes(invocation_context context.Context, before map[string]string) ([]Change, error) {
	output, error := run_step(clone.Run(invocation_context, clone.Project, inside(clone.Path(), MANIFEST_SCRIPT)), nil)
	if error != nil {
		return nil, fmt.Errorf("failed to list the clone: %w", error)
	}
	after, error := parse_manifest(output)
	if error != nil {
		return nil, error
	}

	changes := []Change{}
	for path := range before {
		if _, kept := after[path]; !kept {
			changes = append(changes, Change{Path: path, Deleted: true})
		}
	}
	fetch := []string{}
	for path, signature := range after {
		if before[path] != signature {
			fetch = append(fetch, path)
		}
	}
	if len(fetch) == 0 {
		return changes, nil
	}

	list := strings.Join(fetch, "\x00") + "\x00"
	archive, error := run_step(clone.Run(invocation_context, clone.Project, inside(clone.Path(), ARCHIVE_SCRIPT)), []byte(list))
	if error != nil {
		return nil, fmt.Errorf("failed to read the changed files: %w", error)
	}
	fetched, error := read_archive(archive)
	if error != nil {
		return nil, error
	}
	return append(changes, fetched...), nil
}

func inside(directory string, script string) string {
	return "cd " + shell_quote(directory) + " || exit\n" + script
}

func shell_quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func run_step(command *exec.Cmd, input []byte) ([]byte, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if input != nil {
		command.Stdin = bytes.NewReader(input)
	}
	if error := command.Run(); error != nil {
		var exit_error *exec.ExitError
		if errors.As(error, &exit_error) {
			return nil, &step_failure{stderr: stderr.String(), exit_code: exit_error.ExitCode()}
		}
		return nil, error
	}
	return stdout.Bytes(), nil
}

func parse_manifest(output []byte) (map[string]string, error) {
	manifest := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(output), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, " ", 5)
		if len(fields) != 5 || !strings.HasPrefix(fields[4], "./") {
			return nil, fmt.Errorf("cannot read the clone listing at %q: file names with newlines are not supported", line)
		}
		manifest[strings.TrimPrefix(fields[4], "./")] = strings.Join(fields[:4], " ")
	}
	return manifest, nil
}

func read_archive(archive []byte) ([]Change, error) {
	reader := tar.NewReader(bytes.NewReader(archive))
	changes := []Change{}
	for {
		header, error := reader.Next()
		if error == io.EOF {
			return changes, nil
		}
		if error != nil {
			return nil, fmt.Errorf("failed to read the changed files: %w", error)
		}
		change := Change{Path: strings.TrimPrefix(header.Name, "./"), Mode: header.FileInfo().Mode()}
		switch header.Typeflag {
		case tar.TypeSymlink:
			change.Link = header.Linkname
		case tar.TypeReg:
			change.Content, error = io.ReadAll(reader)
			if error != nil {
				return nil, fmt.Errorf("failed to read %s: %w", change.Path, error)
			}
		default:
			continue
		}
		changes = append(changes, change)
	}
}
