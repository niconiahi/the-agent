package tool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/niconiahi/the-agent/setup"
)

type replayed struct {
	written map[string]string
	deleted []string
}

func recording_replay(record *replayed) Replay {
	record.written = map[string]string{}
	return Replay{
		Write: func(_ context.Context, path string, content string) error {
			record.written[path] = content
			if error := os.MkdirAll(filepath.Dir(path), 0o755); error != nil {
				return error
			}
			return os.WriteFile(path, []byte(content), 0o644)
		},
		Delete: func(_ context.Context, path string) error {
			record.deleted = append(record.deleted, path)
			return os.Remove(path)
		},
	}
}

func run_locally(invocation_context context.Context, directory string, script string) *exec.Cmd {
	command := exec.CommandContext(invocation_context, "/bin/bash", "-c", script)
	command.Dir = directory
	return command
}

func local_project(t *testing.T, files map[string]string) string {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("clonefile needs macOS")
	}
	project, error := filepath.EvalSymlinks(t.TempDir())
	if error != nil {
		t.Fatal(error)
	}
	for name, content := range files {
		path := filepath.Join(project, name)
		if error := os.MkdirAll(filepath.Dir(path), 0o755); error != nil {
			t.Fatal(error)
		}
		if error := os.WriteFile(path, []byte(content), 0o644); error != nil {
			t.Fatal(error)
		}
	}
	return project
}

func local_bash_write(t *testing.T, project string, record *replayed) Tool {
	t.Helper()
	clones := filepath.Join(t.TempDir(), "clones")
	if error := os.Mkdir(clones, 0o700); error != nil {
		t.Fatal(error)
	}
	sandbox := Sandbox{User: "_the-agent", Home: filepath.Dir(clones), Project: project}
	return BashWriteTool(sandbox, Clonefile(project, clones, run_locally), recording_replay(record))
}

func run_bash_write(t *testing.T, bash_write Tool, command string) string {
	t.Helper()
	result, error := bash_write.Execute(context.Background(), "", map[string]interface{}{"command": command})
	if error != nil {
		t.Fatalf("bash_write failed: %v", error)
	}
	return extract_text(t, result)
}

func read_file(t *testing.T, path string) string {
	t.Helper()
	content, error := os.ReadFile(path)
	if error != nil {
		t.Fatal(error)
	}
	return string(content)
}

func TestBashWrite_ReplaysAChangedFile(t *testing.T) {
	project := local_project(t, map[string]string{"go.mod": "module a\n", "main.go": "package main\n"})
	record := &replayed{}

	run_bash_write(t, local_bash_write(t, project, record), "echo 'go 1.25' >> go.mod")

	path := filepath.Join(project, "go.mod")
	if got := record.written[path]; got != "module a\ngo 1.25\n" {
		t.Fatalf("want go.mod replayed with the new line, got %q (all: %v)", got, record.written)
	}
	if len(record.written) != 1 || len(record.deleted) != 0 {
		t.Fatalf("want only go.mod replayed, got written %v deleted %v", record.written, record.deleted)
	}
}

func TestBashWrite_CreatesNewFilesAndDeletesRemovedOnes(t *testing.T) {
	project := local_project(t, map[string]string{"old.go": "package old\n", "kept.go": "package kept\n"})
	record := &replayed{}

	text := run_bash_write(t, local_bash_write(t, project, record), "rm old.go && mkdir -p gen && echo 'package gen' > gen/gen.go")

	if got := read_file(t, filepath.Join(project, "gen", "gen.go")); got != "package gen\n" {
		t.Fatalf("gen/gen.go: %q", got)
	}
	if len(record.deleted) != 1 || record.deleted[0] != filepath.Join(project, "old.go") {
		t.Fatalf("want old.go deleted, got %v", record.deleted)
	}
	if want := "Applied to the project:\nA gen/gen.go\nD old.go"; !strings.HasSuffix(text, want) {
		t.Fatalf("want the model told what was applied, got %q", text)
	}
}

func TestBashWrite_IgnoresChangesUnderGit(t *testing.T) {
	project := local_project(t, map[string]string{".git/HEAD": "ref: refs/heads/main\n", "a.txt": "a\n"})
	record := &replayed{}

	text := run_bash_write(t, local_bash_write(t, project, record), "echo detached > .git/HEAD && touch .git/index")

	if len(record.written) != 0 || len(record.deleted) != 0 {
		t.Fatalf("want nothing replayed, got written %v deleted %v", record.written, record.deleted)
	}
	if got := read_file(t, filepath.Join(project, ".git", "HEAD")); got != "ref: refs/heads/main\n" {
		t.Fatalf(".git/HEAD changed: %q", got)
	}
	if !strings.HasSuffix(text, "No files changed.") {
		t.Fatalf("want No files changed, got %q", text)
	}
}

func TestBashWrite_SkipsFilesRewrittenWithTheSameContent(t *testing.T) {
	project := local_project(t, map[string]string{"a.txt": "same\n"})
	record := &replayed{}

	run_bash_write(t, local_bash_write(t, project, record), "cp a.txt b && mv b a.txt && touch a.txt")

	if len(record.written) != 0 {
		t.Fatalf("want nothing replayed, got %v", record.written)
	}
}

func TestBashWrite_RunsInTheSameClonePathEveryCallAndRemovesIt(t *testing.T) {
	project := local_project(t, map[string]string{"a.txt": "a\n"})
	bash_write := local_bash_write(t, project, &replayed{})

	first := strings.SplitN(run_bash_write(t, bash_write, "pwd -P"), "\n", 2)[0]
	second := strings.SplitN(run_bash_write(t, bash_write, "pwd -P"), "\n", 2)[0]

	if first != second {
		t.Fatalf("want a stable clone path, got %q then %q", first, second)
	}
	if first == project || !strings.HasPrefix(filepath.Base(first), filepath.Base(project)+"-") {
		t.Fatalf("want the command run in a clone named after the project, got %q", first)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		entries, error := os.ReadDir(filepath.Dir(first))
		if error != nil {
			t.Fatal(error)
		}
		if len(entries) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the clone was never removed: %v", entries)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBashWrite_ReportsSymbolicLinksAndBinaryFilesAsNotApplied(t *testing.T) {
	project := local_project(t, map[string]string{"a.txt": "a\n"})
	record := &replayed{}

	text := run_bash_write(t, local_bash_write(t, project, record), `ln -s a.txt link && printf 'x\000y' > blob`)

	if len(record.written) != 0 {
		t.Fatalf("want nothing written, got %v", record.written)
	}
	if want := "Not applied:\nblob (binary file)\nlink (symbolic link)"; !strings.HasSuffix(text, want) {
		t.Fatalf("want %q, got %q", want, text)
	}
}

func TestBashWrite_KeepsTheModeOfAnExecutableFile(t *testing.T) {
	project := local_project(t, map[string]string{"a.txt": "a\n"})

	run_bash_write(t, local_bash_write(t, project, &replayed{}), `printf '#!/bin/sh\n' > run.sh && chmod 755 run.sh`)

	info, error := os.Stat(filepath.Join(project, "run.sh"))
	if error != nil {
		t.Fatal(error)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("want run.sh executable, got %v", info.Mode())
	}
}

func TestBashWrite_LeavesProjectChangesMadeDuringTheCommandAlone(t *testing.T) {
	project := local_project(t, map[string]string{"a.txt": "before\n"})
	record := &replayed{}

	run_bash_write(t, local_bash_write(t, project, record), "echo mine > "+filepath.Join(project, "a.txt")+" && echo mine > "+filepath.Join(project, "new.txt"))

	if len(record.written) != 0 || len(record.deleted) != 0 {
		t.Fatalf("want nothing replayed, got written %v deleted %v", record.written, record.deleted)
	}
	if got := read_file(t, filepath.Join(project, "new.txt")); got != "mine\n" {
		t.Fatalf("new.txt: %q", got)
	}
}

func TestBashWrite_WithoutSetupFailsAtOnceWithTheSetupCommand(t *testing.T) {
	sandbox := Sandbox{User: "_the-agent-missing", Home: setup.Home(), Project: test_project(t)}
	bash_write := BashWriteTool(sandbox, Clonefile(sandbox.Project, filepath.Join(sandbox.Home, "clones"), sandbox.Command), recording_replay(&replayed{}))
	start := time.Now()

	_, error := bash_write.Execute(context.Background(), "", map[string]interface{}{"command": "true"})

	if error == nil || !strings.Contains(error.Error(), "sudo the-agent setup") {
		t.Fatalf("want an error naming sudo the-agent setup, got %v", error)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("sudo -n must fail at once, took %v", elapsed)
	}
}
