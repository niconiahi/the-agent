package session_test

import (
	"reflect"
	"testing"

	"github.com/niconiahi/the-agent/session"
)

func TestThinkingRanges_FindsThinkingBlocksButNotFencesInsideOtherBlocks(t *testing.T) {
	text := "## assistant\n" +
		"\n" +
		"```thinking\n" +
		"first\n" +
		"```\n" +
		"\n" +
		"````markdown\n" +
		"```thinking\n" +
		"quoted, not a block\n" +
		"```\n" +
		"````\n" +
		"\n" +
		"````thinking\n" +
		"has ``` inside\n" +
		"````\n"

	want := [][2]int{{3, 5}, {13, 15}}
	if got := session.ThinkingRanges(text); !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestThinkingRanges_IgnoresAnUnclosedBlock(t *testing.T) {
	if got := session.ThinkingRanges("```thinking\nstill streaming\n"); len(got) != 0 {
		t.Fatalf("want none, got %v", got)
	}
}
