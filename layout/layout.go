package layout

import "path/filepath"

const FOLDER = ".the-agent"
const CLONE = "clone"
const TMP = "tmp"
const SESSIONS = "sessions"
const SYSTEM_PROMPT = "system_prompt.md"
const GITIGNORE = ".gitignore"
const GO_CACHE = "gocache"
const GO_MODULE_CACHE = "gomodcache"

var OWNED = []string{CLONE, TMP}
var CACHES = []string{GO_CACHE, GO_MODULE_CACHE}

func Folder(project string) string {
	return filepath.Join(project, FOLDER)
}

func Clone(project string) string {
	return filepath.Join(project, FOLDER, CLONE)
}

func Tmp(project string) string {
	return filepath.Join(project, FOLDER, TMP)
}
