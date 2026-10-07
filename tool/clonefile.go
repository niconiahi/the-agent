package tool

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

const MANIFEST_SCRIPT = `find . -name .git -prune -o \( -type f -o -type l \) -exec stat -f '%i %z %Fm %p %N' {} +`

type Runner func(invocation_context context.Context, directory string, script string) *exec.Cmd

type step_failure struct {
	stderr    string
	exit_code int
}

func (failure *step_failure) Error() string {
	return fmt.Sprintf("exit status %d: %s", failure.exit_code, strings.TrimSpace(failure.stderr))
}

type clonefile_clone struct {
	run     Runner
	project string
	clones  string
	path    string
	before  map[string]string
}

func ClonePath(project string, clones string) string {
	sum := sha256.Sum256([]byte(project))
	return filepath.Join(clones, filepath.Base(project)+"-"+hex.EncodeToString(sum[:])[:12])
}

func Clonefile(project string, clones string, run Runner) Cloner {
	return func(invocation_context context.Context) (Clone, error) {
		clone := &clonefile_clone{run: run, project: project, clones: clones, path: ClonePath(project, clones)}
		if error := clone.discard(invocation_context); error != nil {
			return nil, error
		}
		script := "cp -c -R " + shell_quote(project) + " " + shell_quote(clone.path) + " || exit\n" + inside(clone.path, MANIFEST_SCRIPT)
		output, error := run_step(run(invocation_context, project, script), nil)
		if error != nil {
			return nil, fmt.Errorf("failed to clone %s: %w", project, error)
		}
		clone.before, error = parse_manifest(output)
		if error != nil {
			return nil, error
		}
		return clone, nil
	}
}

func (clone *clonefile_clone) Command(invocation_context context.Context, script string) *exec.Cmd {
	return clone.run(invocation_context, clone.project, inside(clone.path, script))
}

func (clone *clonefile_clone) Changes(invocation_context context.Context) ([]Change, error) {
	output, error := run_step(clone.run(invocation_context, clone.project, inside(clone.path, MANIFEST_SCRIPT)), nil)
	if error != nil {
		return nil, fmt.Errorf("failed to list the clone: %w", error)
	}
	after, error := parse_manifest(output)
	if error != nil {
		return nil, error
	}

	changes := []Change{}
	for path := range clone.before {
		if _, kept := after[path]; !kept {
			changes = append(changes, Change{Path: path, Deleted: true})
		}
	}
	fetch := []string{}
	for path, signature := range after {
		if clone.before[path] != signature {
			fetch = append(fetch, path)
		}
	}
	if len(fetch) == 0 {
		return changes, nil
	}

	list := strings.Join(fetch, "\x00") + "\x00"
	archive, error := run_step(clone.run(invocation_context, clone.project, inside(clone.path, "tar -c -f - --no-mac-metadata --null -T -")), []byte(list))
	if error != nil {
		return nil, fmt.Errorf("failed to read the changed files: %w", error)
	}
	fetched, error := read_archive(archive)
	if error != nil {
		return nil, error
	}
	return append(changes, fetched...), nil
}

func (clone *clonefile_clone) Remove() {
	clone.discard(context.Background())
}

func (clone *clonefile_clone) discard(invocation_context context.Context) error {
	path := shell_quote(clone.path)
	script := "if [ -e " + path + " ] || [ -L " + path + " ]; then trash=$(mktemp -d " + shell_quote(filepath.Join(clone.clones, ".removing.XXXXXX")) + ") && mv " + path + " \"$trash\"/ && echo \"$trash\"; fi"
	output, error := run_step(clone.run(invocation_context, clone.project, script), nil)
	if error != nil {
		return fmt.Errorf("failed to set the previous clone aside: %w", error)
	}
	trash := strings.TrimSpace(string(output))
	if trash == "" {
		return nil
	}
	removal := clone.run(context.Background(), clone.project, "rm -rf "+shell_quote(trash))
	if error := removal.Start(); error != nil {
		return nil
	}
	go removal.Wait()
	return nil
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
