package clone_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/clone"
)

func project_with(t *testing.T, files map[string]string) string {
	t.Helper()
	project, error := filepath.EvalSymlinks(t.TempDir())
	if error != nil {
		t.Fatal(error)
	}
	write_files(t, project, files)
	if error := os.MkdirAll(clone.Path(project), 0o700); error != nil {
		t.Fatal(error)
	}
	return project
}

func write_files(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, name)
		if error := os.MkdirAll(filepath.Dir(path), 0o755); error != nil {
			t.Fatal(error)
		}
		if error := os.WriteFile(path, []byte(content), 0o644); error != nil {
			t.Fatal(error)
		}
	}
}

func sync(t *testing.T, project string) clone.Report {
	t.Helper()
	report, error := clone.Sync(project, clone.Path(project))
	if error != nil {
		t.Fatalf("sync failed: %v", error)
	}
	return report
}

func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	error := filepath.Walk(root, func(path string, info os.FileInfo, error error) error {
		if error != nil {
			return error
		}
		relative, _ := filepath.Rel(root, path)
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, error := os.Readlink(path)
			if error != nil {
				return error
			}
			files[relative] = "-> " + target
		case info.Mode().IsRegular():
			content, error := os.ReadFile(path)
			if error != nil {
				return error
			}
			files[relative] = info.Mode().Perm().String() + " " + string(content)
		}
		return nil
	})
	if error != nil {
		t.Fatal(error)
	}
	return files
}

func same_trees(t *testing.T, project string) {
	t.Helper()
	want := tree(t, project)
	for path := range want {
		if strings.HasPrefix(path, ".the-agent"+string(filepath.Separator)) {
			delete(want, path)
		}
	}
	got := tree(t, clone.Path(project))
	if len(want) != len(got) {
		t.Fatalf("want the clone to hold %v, got %v", want, got)
	}
	for path, content := range want {
		if got[path] != content {
			t.Fatalf("%s: want %q in the clone, got %q", path, content, got[path])
		}
	}
}

func TestSync_CopiesTheProjectWithItsGitButWithoutTheAgentFolder(t *testing.T) {
	project := project_with(t, map[string]string{
		"main.go":                          "package main\n",
		"nested/deeper/file.txt":           "deep\n",
		".git/HEAD":                        "ref: refs/heads/main\n",
		".the-agent/sessions/a/session.md": "# a\n",
	})

	sync(t, project)

	same_trees(t, project)
	if _, error := os.Stat(filepath.Join(clone.Path(project), ".the-agent")); !os.IsNotExist(error) {
		t.Fatalf("the clone must not hold .the-agent: %v", error)
	}
}

func TestSync_ASecondSyncCopiesOnlyFilesChangedSinceTheFirst(t *testing.T) {
	project := project_with(t, map[string]string{
		"same.txt":     "same\n",
		"resized.txt":  "short\n",
		"touched.txt":  "touched\n",
		"chmodded.txt": "chmodded\n",
		"nested/x.txt": "x\n",
	})
	sync(t, project)
	write_files(t, project, map[string]string{"resized.txt": "a longer line\n", "added/new.txt": "new\n"})
	info, error := os.Stat(filepath.Join(project, "touched.txt"))
	if error != nil {
		t.Fatal(error)
	}
	later := info.ModTime().Add(1)
	if error := os.Chtimes(filepath.Join(project, "touched.txt"), later, later); error != nil {
		t.Fatal(error)
	}
	if error := os.Chmod(filepath.Join(project, "chmodded.txt"), 0o755); error != nil {
		t.Fatal(error)
	}

	report := sync(t, project)

	want := []string{"added/new.txt", "chmodded.txt", "resized.txt", "touched.txt"}
	if got := sorted(report.Copied); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("want only %v copied, got %v", want, got)
	}
	same_trees(t, project)
}

func TestSync_UndoesWhatACommandDidInTheClone(t *testing.T) {
	project := project_with(t, map[string]string{
		"edited.txt":       "original\n",
		"becomes_dir":      "a file\n",
		"gone/inside.txt":  "inside\n",
		"locked/kept.txt":  "kept\n",
		"deleted/file.txt": "deleted\n",
	})
	sync(t, project)
	cloned := clone.Path(project)
	write_files(t, cloned, map[string]string{
		"edited.txt":         "the command's\n",
		"new.txt":            "new\n",
		"built/out/bin.txt":  "built\n",
		"locked/created.txt": "created\n",
	})
	steps := []error{
		os.Remove(filepath.Join(cloned, "deleted", "file.txt")),
		os.Remove(filepath.Join(cloned, "becomes_dir")),
		os.Mkdir(filepath.Join(cloned, "becomes_dir"), 0o755),
		os.Chmod(filepath.Join(cloned, "locked"), 0o555),
		os.Chmod(filepath.Join(cloned, "built", "out"), 0o555),
		os.RemoveAll(filepath.Join(project, "gone")),
	}
	for _, error := range steps {
		if error != nil {
			t.Fatal(error)
		}
	}

	report := sync(t, project)

	same_trees(t, project)
	want := []string{"built", "locked/created.txt", "new.txt"}
	if got := sorted(report.Removed); !contains_all(got, want) {
		t.Fatalf("want %v among the removed, got %v", want, got)
	}
}

func TestSync_CopiesSymbolicLinksAsLinks(t *testing.T) {
	project := project_with(t, map[string]string{"target.txt": "target\n", "folder/file.txt": "file\n"})
	for name, target := range map[string]string{"link.txt": "target.txt", "folder_link": "folder", "dangling": "missing"} {
		if error := os.Symlink(target, filepath.Join(project, name)); error != nil {
			t.Fatal(error)
		}
	}
	sync(t, project)
	if error := os.Remove(filepath.Join(project, "link.txt")); error != nil {
		t.Fatal(error)
	}
	if error := os.Symlink("folder/file.txt", filepath.Join(project, "link.txt")); error != nil {
		t.Fatal(error)
	}

	sync(t, project)

	same_trees(t, project)
}

func TestSync_ASyncInterruptedHalfwayIsCompletedByTheNextOne(t *testing.T) {
	files := map[string]string{"blocked/file.txt": "blocked\n"}
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		files[name+"/file.txt"] = name + "\n"
		files[name+".txt"] = name + "\n"
	}
	project := project_with(t, files)
	blocked := filepath.Join(project, "blocked", "file.txt")
	if error := os.Chmod(blocked, 0); error != nil {
		t.Fatal(error)
	}
	if _, error := clone.Sync(project, clone.Path(project)); error == nil {
		t.Skip("the unreadable file was read anyway (running as root?)")
	}
	if error := os.Chmod(blocked, 0o644); error != nil {
		t.Fatal(error)
	}

	sync(t, project)

	same_trees(t, project)
}

func TestSync_ClonesFilesWhereTheFilesystemCanAndCopiesThemOtherwise(t *testing.T) {
	project := project_with(t, map[string]string{"a.txt": "a\n", "nested/b.txt": "b\n"})
	copy_on_write, known := copy_on_write_filesystem(t, project)
	if !known {
		t.Skip("unknown whether this filesystem supports copy-on-write")
	}

	report := sync(t, project)

	same_trees(t, project)
	if copy_on_write && len(report.Cloned) != 2 {
		t.Fatalf("want both files cloned, got %+v", report)
	}
	if !copy_on_write && (len(report.Cloned) != 0 || len(report.Copied) != 2) {
		t.Fatalf("want both files copied without cloning, got %+v", report)
	}
}

func contains_all(paths []string, wanted []string) bool {
	for _, want := range wanted {
		found := false
		for _, path := range paths {
			found = found || path == want
		}
		if !found {
			return false
		}
	}
	return true
}

func sorted(paths []string) []string {
	result := append([]string{}, paths...)
	sort.Strings(result)
	return result
}
