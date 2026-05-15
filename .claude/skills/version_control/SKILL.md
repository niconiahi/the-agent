# GitHub Conventions

## Version Control

The user controls all version control operations. Never run:

- `git add`
- `git commit`
- `git push`
- `git stash`
- `git reset`
- Any other git command that modifies state

Only read-only git commands are allowed (e.g., `git status`, `git log`, `git diff`).

