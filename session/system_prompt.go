package session

import (
	"regexp"
	"strings"
	"time"
)

const ROOT_SYSTEM_PROMPT_PATH = "../../system_prompt.md"

const SYSTEM_PROMPT_LINK = "[system_prompt.md](" + ROOT_SYSTEM_PROMPT_PATH + ")"

const CREATED_PREFIX = "created · "

var link_pattern = regexp.MustCompile(`(?:^|[^!])\[[^\]]*\]\(([^)\s]+)\)`)

var created_pattern = regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(CREATED_PREFIX) + `\S+[ \t]*$`)

const TASK_CALL_PREFIX = "task call · "

var task_call_pattern = regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(TASK_CALL_PREFIX) + `(\S+)[ \t]*$`)

func New(at time.Time) string {
	return NewLinked(ROOT_SYSTEM_PROMPT_PATH, at)
}

func NewLinked(link string, at time.Time) string {
	return "[system_prompt.md](" + link + ")\n\n" + CREATED_PREFIX + stamp(at) + "\n\n## user\n\n"
}

func NewChild(link string, call string, at time.Time) string {
	return "[system_prompt.md](" + link + ")\n\n" + CREATED_PREFIX + stamp(at) + "\n\n" + TASK_CALL_PREFIX + call + "\n\n## user\n\n"
}

type Origin struct {
	Call string
	Job  string
}

func (parsed *Session) Origin() (Origin, bool) {
	match := task_call_pattern.FindStringSubmatch(parsed.preamble)
	if match == nil {
		return Origin{}, false
	}
	origin := Origin{Call: match[1]}
	for _, current := range parsed.turns {
		if current.role == ROLE_USER {
			origin.Job = strings.TrimSpace(current.body)
			break
		}
	}
	return origin, true
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
