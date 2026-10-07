package partialjson_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/partialjson"
)

func TestRead_ReturnsTheFieldsCompleteSoFarAndThePartialValueStreaming(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		complete  map[string]string
		streaming string
		partial   string
	}{
		{name: "nothing yet", text: "", complete: map[string]string{}},
		{name: "only the brace", text: "{", complete: map[string]string{}},
		{name: "split mid-key", text: `{"pa`, complete: map[string]string{}},
		{name: "key without its colon", text: `{"path"`, complete: map[string]string{}},
		{name: "key without its value", text: `{"path":`, complete: map[string]string{}},
		{name: "value just opened", text: `{"path":"`, complete: map[string]string{}, streaming: "path", partial: ""},
		{name: "value streaming", text: `{"path":"src/a.g`, complete: map[string]string{}, streaming: "path", partial: "src/a.g"},
		{name: "value complete", text: `{"path":"src/a.go"`, complete: map[string]string{"path": "src/a.go"}},
		{name: "second key split mid-key", text: `{"path":"a.go","old_te`, complete: map[string]string{"path": "a.go"}},
		{
			name:      "second value streaming",
			text:      `{"path":"a.go","old_text":"func main`,
			complete:  map[string]string{"path": "a.go"},
			streaming: "old_text",
			partial:   "func main",
		},
		{
			name:     "whitespace between tokens",
			text:     "{ \"path\" :\n \"a.go\" , \"old_text\" : \"x\" }",
			complete: map[string]string{"path": "a.go", "old_text": "x"},
		},
		{name: "split right after a backslash", text: `{"new_text":"a\`, complete: map[string]string{}, streaming: "new_text", partial: "a"},
		{name: "simple escapes", text: `{"new_text":"a\n\t\"q\"\\/\/`, complete: map[string]string{}, streaming: "new_text", partial: "a\n\t\"q\"\\//"},
		{name: "split mid unicode escape", text: `{"new_text":"caf\u00`, complete: map[string]string{}, streaming: "new_text", partial: "caf"},
		{name: "whole unicode escape", text: `{"new_text":"caf\u00e9`, complete: map[string]string{}, streaming: "new_text", partial: "café"},
		{name: "split between a surrogate pair", text: `{"new_text":"x\ud83d`, complete: map[string]string{}, streaming: "new_text", partial: "x"},
		{name: "split inside the second surrogate", text: `{"new_text":"x\ud83d\ude`, complete: map[string]string{}, streaming: "new_text", partial: "x"},
		{name: "whole surrogate pair", text: `{"new_text":"x\ud83d\ude00`, complete: map[string]string{}, streaming: "new_text", partial: "x😀"},
		{name: "split mid UTF-8 sequence", text: "{\"new_text\":\"caf\xc3", complete: map[string]string{}, streaming: "new_text", partial: "caf"},
		{name: "split mid four-byte UTF-8 sequence", text: "{\"new_text\":\"x\xf0\x9f\x98", complete: map[string]string{}, streaming: "new_text", partial: "x"},
		{name: "raw UTF-8 complete", text: "{\"new_text\":\"café ✓\"", complete: map[string]string{"new_text": "café ✓"}},
		{name: "escaped key", text: `{"a\"b":"c"`, complete: map[string]string{`a"b`: "c"}},
		{
			name:     "values that are not strings are skipped",
			text:     `{"count":12,"flag":true,"none":null,"list":["path",{"path":"no"}],"nested":{"path":"no","deep":[1,"}"]},"path":"yes"`,
			complete: map[string]string{"path": "yes"},
		},
		{name: "split inside a skipped value", text: `{"nested":{"path":"no`, complete: map[string]string{}},
		{
			name:     "whole document",
			text:     `{"path":"a.go","old_text":"one","new_text":"two"}`,
			complete: map[string]string{"path": "a.go", "old_text": "one", "new_text": "two"},
		},
		{name: "not an object", text: `["path"`, complete: map[string]string{}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := partialjson.Read(test.text)
			if !reflect.DeepEqual(got.Complete, test.complete) {
				t.Errorf("complete: got %q, want %q", got.Complete, test.complete)
			}
			if got.Streaming != test.streaming || got.Partial != test.partial {
				t.Errorf("streaming: got %q=%q, want %q=%q", got.Streaming, got.Partial, test.streaming, test.partial)
			}
		})
	}
}

func TestRead_EveryPrefixAgreesWithTheWholeDocument(t *testing.T) {
	arguments := map[string]any{
		"path":     "src/naïve \"quoted\".go",
		"old_text": "func old() {\n\treturn \"\\n\" // ✓\n}",
		"new_text": "func new() {\n\treturn \"é😀\\u0041\" <tag> & \x01\n}\n",
	}
	encoded, error := json.Marshal(arguments)
	if error != nil {
		t.Fatal(error)
	}
	text := string(encoded)
	for end := 0; end <= len(text); end++ {
		got := partialjson.Read(text[:end])
		for key, value := range got.Complete {
			if value != arguments[key] {
				t.Fatalf("prefix %q: complete %s = %q, want %q", text[:end], key, value, arguments[key])
			}
		}
		if got.Streaming != "" {
			if _, done := got.Complete[got.Streaming]; done {
				t.Fatalf("prefix %q: %s is both complete and streaming", text[:end], got.Streaming)
			}
			whole, _ := arguments[got.Streaming].(string)
			if !strings.HasPrefix(whole, got.Partial) {
				t.Fatalf("prefix %q: partial %s = %q is not a prefix of %q", text[:end], got.Streaming, got.Partial, whole)
			}
		}
	}
	if got := partialjson.Read(text); len(got.Complete) != 3 {
		t.Fatalf("whole document: %q", got.Complete)
	}
}
