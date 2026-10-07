package session

import (
	"regexp"
	"strings"
	"time"
)

const SYSTEM_PROMPT_LINK = "[system_prompt.md](../../system_prompt.md)"

const CREATED_PREFIX = "created · "

var link_pattern = regexp.MustCompile(`(?:^|[^!])\[[^\]]*\]\(([^)\s]+)\)`)

var created_pattern = regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(CREATED_PREFIX) + `\S+[ \t]*$`)

func New(at time.Time) string {
	return SYSTEM_PROMPT_LINK + "\n\n" + CREATED_PREFIX + stamp(at) + "\n\n## user\n\n"
}

// NewLinked is New for a session.md that reaches system_prompt.md through
// link, such as a subagent's, which sits deeper than a root session.
func NewLinked(link string, at time.Time) string {
	return "[system_prompt.md](" + link + ")\n\n" + CREATED_PREFIX + stamp(at) + "\n\n## user\n\n"
}

func (parsed *Session) SystemPromptLink() (string, bool) {
	match := link_pattern.FindStringSubmatch(parsed.preamble)
	if match == nil {
		return "", false
	}
	return match[1], true
}

func (parsed *Session) SystemPrompt(linked string) string {
	parts := []string{}
	if linked = strings.TrimSpace(linked); linked != "" {
		parts = append(parts, linked)
	}
	if created := created_pattern.FindString(parsed.preamble); created != "" {
		parts = append(parts, strings.TrimSpace(created))
	}
	return strings.Join(parts, "\n\n")
}
