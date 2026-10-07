package tool

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"github.com/niconiahi/the-agent/layout"
	"github.com/niconiahi/the-agent/setup"
)

const FOLDERS_SCRIPT = `find . -mindepth 1 \( -path ./.the-agent -o -name .git \) -prune -o -type d -print`

type Runner func(invocation_context context.Context, directory string, script string) *exec.Cmd

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

type manifest struct {
	files   map[string]string
	folders map[string]bool
}

var project_locks sync.Map

func (clone Clone) lock() func() {
	value, _ := project_locks.LoadOrStore(clone.Project, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

func (clone Clone) sync(invocation_context context.Context) (manifest, error) {
	script := setup.Quote(clone.Binary) + " sync " + setup.Quote(clone.Project) + " || exit\n" + inside(layout.Clone(clone.Project), listing_script())
	output, error := run_step(clone.Run(invocation_context, clone.Project, script), nil)
	if error != nil {
		return manifest{}, fmt.Errorf("failed to sync the clone: %w", error)
	}
	return parse_manifest(output)
}

func (clone Clone) command(invocation_context context.Context, script string) *exec.Cmd {
	return clone.Run(invocation_context, clone.Project, inside(layout.Clone(clone.Project), script))
}

func (clone Clone) changes(invocation_context context.Context, before manifest) ([]Change, error) {
	output, error := run_step(clone.Run(invocation_context, clone.Project, inside(layout.Clone(clone.Project), listing_script())), nil)
	if error != nil {
		return nil, fmt.Errorf("failed to list the clone: %w", error)
	}
	after, error := parse_manifest(output)
	if error != nil {
		return nil, error
	}

	changes := []Change{}
	for path := range before.folders {
		if !after.folders[path] {
			changes = append(changes, Change{Path: path, Deleted: true, Folder: true})
		}
	}
	for path := range before.files {
		if _, kept := after.files[path]; !kept {
			changes = append(changes, Change{Path: path, Deleted: true})
		}
	}
	fetch := []string{}
	for path, signature := range after.files {
		if before.files[path] != signature {
			fetch = append(fetch, path)
		}
	}
	if len(fetch) == 0 {
		return changes, nil
	}

	list := strings.Join(fetch, "\x00") + "\x00"
	archive, error := run_step(clone.Run(invocation_context, clone.Project, inside(layout.Clone(clone.Project), ARCHIVE_SCRIPT)), []byte(list))
	if error != nil {
		return nil, fmt.Errorf("failed to read the changed files: %w", error)
	}
	fetched, error := read_archive(archive)
	if error != nil {
		return nil, error
	}
	return append(changes, fetched...), nil
}

func listing_script() string {
	return MANIFEST_SCRIPT + " && " + FOLDERS_SCRIPT
}

func inside(directory string, script string) string {
	return "cd " + setup.Quote(directory) + " || exit\n" + script
}

type captured struct {
	stdout    []byte
	stderr    string
	exit_code int
}

func capture(command *exec.Cmd, input []byte) (captured, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if input != nil {
		command.Stdin = bytes.NewReader(input)
	}
	error := command.Run()
	result := captured{stdout: stdout.Bytes(), stderr: stderr.String()}
	if error == nil {
		return result, nil
	}
	var exit_error *exec.ExitError
	if !errors.As(error, &exit_error) {
		return result, error
	}
	result.exit_code = exit_error.ExitCode()
	return result, nil
}

func run_step(command *exec.Cmd, input []byte) ([]byte, error) {
	result, error := capture(command, input)
	if error != nil {
		return nil, error
	}
	if result.exit_code != 0 {
		return nil, &step_failure{stderr: result.stderr, exit_code: result.exit_code}
	}
	return result.stdout, nil
}

func parse_manifest(output []byte) (manifest, error) {
	parsed := manifest{files: map[string]string{}, folders: map[string]bool{}}
	for _, line := range strings.Split(strings.TrimSuffix(string(output), "\n"), "\n") {
		if line == "" {
			continue
		}
		if folder, found := strings.CutPrefix(line, "./"); found {
			parsed.folders[folder] = true
			continue
		}
		fields := strings.SplitN(line, " ", 5)
		if len(fields) != 5 || !strings.HasPrefix(fields[4], "./") {
			return manifest{}, fmt.Errorf("cannot read the clone listing at %q: file names with newlines are not supported", line)
		}
		parsed.files[strings.TrimPrefix(fields[4], "./")] = strings.Join(fields[:4], " ")
	}
	return parsed, nil
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
