package session

import "regexp"

// SYSTEM_PROMPT_LINK is the first line of a new root session: a link, relative
// to .the-agent/sessions/<name>/session.md, to the shared system prompt.
const SYSTEM_PROMPT_LINK = "[system_prompt.md](../../system_prompt.md)"

// A markdown link that is not an image: [text](target).
var link_pattern = regexp.MustCompile(`(?:^|[^!])\[[^\]]*\]\(([^)\s]+)\)`)

// SystemPromptLink is the target of the first link before the first turn,
// relative to the session file. That file's contents are the system prompt.
// It reports false when the session links no system prompt.
func (parsed *Session) SystemPromptLink() (string, bool) {
	match := link_pattern.FindStringSubmatch(parsed.preamble)
	if match == nil {
		return "", false
	}
	return match[1], true
}
