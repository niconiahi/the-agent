package tool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/niconiahi/the-agent/clone"
	"github.com/niconiahi/the-agent/setup"
)

func TestMain(main *testing.M) {
	if len(os.Args) >= 2 && os.Args[1] == "sync" {
		if error := clone.Run(os.Args[2:]); error != nil {
			os.Stderr.WriteString(error.Error() + "\n")
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(main.Run())
}

func test_binary(t *testing.T) string {
	t.Helper()
	binary, error := os.Executable()
	if error != nil {
		t.Fatal(error)
	}
	return binary
}

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
	if error := os.MkdirAll(clone.Path(project), 0o700); error != nil {
		t.Fatal(error)
	}
	return project
}

func local_bash_write(t *testing.T, project string, record *replayed) Tool {
	t.Helper()
	sandbox := Sandbox{User: "_the-agent", Home: t.TempDir(), Project: project}
	return BashWriteTool(sandbox, Clone{Project: project, Binary: test_binary(t), Run: run_locally}, recording_replay(record))
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

func TestBashWrite_RunsInTheProjectsCloneAndKeepsItBetweenCalls(t *testing.T) {
	project := local_project(t, map[string]string{"a.txt": "a\n"})
	bash_write := local_bash_write(t, project, &replayed{})

	first := strings.SplitN(run_bash_write(t, bash_write, "pwd -P"), "\n", 2)[0]
	second := strings.SplitN(run_bash_write(t, bash_write, "pwd -P"), "\n", 2)[0]

	if want := clone.Path(project); first != want || second != want {
		t.Fatalf("want both commands run in %s, got %q then %q", want, first, second)
	}
	if got := read_file(t, filepath.Join(clone.Path(project), "a.txt")); got != "a\n" {
		t.Fatalf("want the clone kept after the call, a.txt holds %q", got)
	}
}

func TestBashWrite_StartsEveryCallFromTheProjectAsItIsNow(t *testing.T) {
	project := local_project(t, map[string]string{"a.txt": "a\n"})
	bash_write := local_bash_write(t, project, &replayed{})
	run_bash_write(t, bash_write, `printf 'x\000y' > blob && echo changed > a.txt`)
	if error := os.WriteFile(filepath.Join(project, "mine.txt"), []byte("mine\n"), 0o644); error != nil {
		t.Fatal(error)
	}

	text := run_bash_write(t, bash_write, "ls; cat a.txt")

	if want := "a.txt\nmine.txt\nchanged\n"; !strings.HasPrefix(text, want) {
		t.Fatalf("want the clone to match the project (no blob, my new file), got %q", text)
	}
}

func TestBashWrite_CallsOnOneProjectRunOneAfterTheOther(t *testing.T) {
	project := local_project(t, map[string]string{"a.txt": "a\n"})
	first := local_bash_write(t, project, &replayed{})
	second := local_bash_write(t, project, &replayed{})
	command := "mkdir running || exit 1; sleep 0.3; rmdir running"
	var group sync.WaitGroup
	exit_codes := make([]interface{}, 2)
	for index, bash_write := range []Tool{first, second} {
		group.Add(1)
		go func() {
			defer group.Done()
			result, error := bash_write.Execute(context.Background(), "", map[string]interface{}{"command": command})
			if error != nil {
				t.Errorf("bash_write failed: %v", error)
				return
			}
			exit_codes[index] = result.Details.(map[string]interface{})["exit_code"]
		}()
	}
	group.Wait()

	for index, exit_code := range exit_codes {
		if exit_code != 0 {
			t.Fatalf("call %d overlapped the other one: exit code %v", index, exit_code)
		}
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
	bash_write := BashWriteTool(sandbox, Clone{Project: sandbox.Project, Binary: test_binary(t), Run: sandbox.Command}, recording_replay(&replayed{}))
	start := time.Now()

	_, error := bash_write.Execute(context.Background(), "", map[string]interface{}{"command": "true"})

	if error == nil || !strings.Contains(error.Error(), "sudo the-agent setup") {
		t.Fatalf("want an error naming sudo the-agent setup, got %v", error)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("sudo -n must fail at once, took %v", elapsed)
	}
}
