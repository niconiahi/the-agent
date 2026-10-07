package session

import "regexp"

const SYSTEM_PROMPT_LINK = "[system_prompt.md](../../system_prompt.md)"

var link_pattern = regexp.MustCompile(`(?:^|[^!])\[[^\]]*\]\(([^)\s]+)\)`)

func (parsed *Session) SystemPromptLink() (string, bool) {
	match := link_pattern.FindStringSubmatch(parsed.preamble)
	if match == nil {
		return "", false
	}
	return match[1], true
}
