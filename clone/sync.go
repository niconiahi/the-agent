package clone

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const FOLDER = ".the-agent"

type Report struct {
	Copied  []string
	Cloned  []string
	Removed []string
}

func Path(project string) string {
	return filepath.Join(project, FOLDER, "clone")
}

// Run is the hidden "the-agent sync <project>" subcommand, which bash_write
// runs as _the-agent. It writes nothing to stdout, which carries the clone's
// listing that follows it.
func Run(arguments []string) error {
	if len(arguments) != 1 || !filepath.IsAbs(arguments[0]) {
		return errors.New("usage: the-agent sync <absolute project path>")
	}
	project := filepath.Clean(arguments[0])
	if _, error := Sync(project, Path(project)); error != nil {
		return fmt.Errorf("failed to sync %s: %w", Path(project), error)
	}
	return nil
}

// Sync brings clone up to date with project, file by file. It skips the
// project's .the-agent folder, keeps .git, copies only what differs in size,
// nanosecond mtime or mode, and removes what the project no longer has. Each
// step leaves a state the next sync can finish from, so an interrupted sync
// is completed by the next one.
func Sync(project string, clone string) (Report, error) {
	report := Report{}
	if error := restore_directory(clone); error != nil {
		return report, error
	}
	error := sync_directory(project, clone, "", &report)
	return report, error
}

func sync_directory(project string, clone string, relative string, report *Report) error {
	sources, error := entries(filepath.Join(project, relative), relative == "")
	if error != nil {
		return error
	}
	destinations, error := entries(filepath.Join(clone, relative), false)
	if error != nil {
		return error
	}

	for name, current := range destinations {
		path := filepath.Join(relative, name)
		if source, kept := sources[name]; kept && kind(source) == kind(current) {
			continue
		}
		if error := remove(filepath.Join(clone, path)); error != nil {
			return fmt.Errorf("failed to remove %s from the clone: %w", path, error)
		}
		if _, kept := sources[name]; !kept {
			report.Removed = append(report.Removed, path)
		}
		delete(destinations, name)
	}

	for name, source := range sources {
		path := filepath.Join(relative, name)
		destination := filepath.Join(clone, path)
		current, exists := destinations[name]
		switch kind(source) {
		case fs.ModeDir:
			if !exists {
				if error := os.Mkdir(destination, 0o777); error != nil {
					return error
				}
			}
			if error := restore_directory(destination); error != nil {
				return error
			}
			if error := sync_directory(project, clone, path, report); error != nil {
				return error
			}
		case fs.ModeSymlink:
			target, error := os.Readlink(filepath.Join(project, path))
			if error != nil {
				return error
			}
			if exists {
				if current_target, error := os.Readlink(destination); error == nil && current_target == target {
					continue
				}
				if error := remove(destination); error != nil {
					return error
				}
			}
			if error := os.Symlink(target, destination); error != nil {
				return fmt.Errorf("failed to link %s: %w", path, error)
			}
			report.Copied = append(report.Copied, path)
		default:
			if exists && same_file(source, current) {
				continue
			}
			if exists {
				if error := remove(destination); error != nil {
					return error
				}
			}
			cloned, error := copy_file(filepath.Join(project, path), destination, source)
			if error != nil {
				return fmt.Errorf("failed to copy %s: %w", path, error)
			}
			report.Copied = append(report.Copied, path)
			if cloned {
				report.Cloned = append(report.Cloned, path)
			}
		}
	}
	return nil
}

func entries(directory string, top bool) (map[string]fs.FileInfo, error) {
	listed, error := os.ReadDir(directory)
	if error != nil {
		return nil, error
	}
	result := map[string]fs.FileInfo{}
	for _, entry := range listed {
		if top && entry.Name() == FOLDER {
			continue
		}
		info, error := entry.Info()
		if errors.Is(error, fs.ErrNotExist) {
			continue
		}
		if error != nil {
			return nil, error
		}
		if kind(info) == fs.ModeIrregular {
			continue
		}
		result[entry.Name()] = info
	}
	return result, nil
}

func kind(info fs.FileInfo) fs.FileMode {
	switch {
	case info.IsDir():
		return fs.ModeDir
	case info.Mode()&fs.ModeSymlink != 0:
		return fs.ModeSymlink
	case info.Mode().IsRegular():
		return 0
	}
	return fs.ModeIrregular
}

func permissions(info fs.FileInfo) fs.FileMode {
	return info.Mode() & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
}

func same_file(source fs.FileInfo, current fs.FileInfo) bool {
	return permissions(current) == permissions(source) && current.Size() == source.Size() && current.ModTime().Equal(source.ModTime())
}

// restore_directory gives a clone folder back the bits the sync and I need:
// the owner's rwx, so a command's chmod 0555 can't stop the next sync, and on
// Linux the group bits, which are the ACL mask that caps my inherited entry.
func restore_directory(directory string) error {
	info, error := os.Lstat(directory)
	if error != nil {
		return error
	}
	if info.Mode().Perm()&DIRECTORY_BITS == DIRECTORY_BITS {
		return nil
	}
	return os.Chmod(directory, info.Mode().Perm()|DIRECTORY_BITS)
}

func remove(path string) error {
	filepath.WalkDir(path, func(current string, entry fs.DirEntry, error error) error {
		if error == nil && entry.IsDir() {
			restore_directory(current)
		}
		return nil
	})
	return os.RemoveAll(path)
}

func finish_copy(destination string, info fs.FileInfo) error {
	if error := os.Chmod(destination, permissions(info)); error != nil {
		return error
	}
	return os.Chtimes(destination, info.ModTime(), info.ModTime())
}

func plain_copy(source string, destination string, info fs.FileInfo) error {
	input, error := os.Open(source)
	if error != nil {
		return error
	}
	defer input.Close()
	output, error := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if error != nil {
		return error
	}
	if _, error := io.Copy(output, input); error != nil {
		output.Close()
		return error
	}
	if error := output.Close(); error != nil {
		return error
	}
	return finish_copy(destination, info)
}
