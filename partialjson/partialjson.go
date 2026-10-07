// Package partialjson reads the string fields of a JSON object that is still
// streaming: given the argument fragments received so far, it returns the
// fields whose values are complete and the decoded prefix of the one whose
// value is arriving. It never returns half an escape, half a surrogate pair
// or half a UTF-8 sequence, so a prefix can always be shown as is.
package partialjson

import (
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type Fields struct {
	// Complete holds every string field whose closing quote has arrived.
	// Fields whose values are not strings are skipped.
	Complete map[string]string
	// Streaming is the key whose string value has opened but not closed,
	// "" when no value is streaming.
	Streaming string
	// Partial is the decoded value of Streaming so far.
	Partial string
}

// Read returns the fields of the top-level object in text, the
// concatenation of every fragment received so far.
func Read(text string) Fields {
	fields := Fields{Complete: map[string]string{}}
	current := &reader{text: text}
	current.space()
	if !current.take('{') {
		return fields
	}
	for {
		current.space()
		if current.ended() || current.peek() == '}' {
			return fields
		}
		if current.take(',') {
			continue
		}
		if current.peek() != '"' {
			return fields
		}
		key, complete := current.string()
		if !complete {
			return fields
		}
		current.space()
		if !current.take(':') {
			return fields
		}
		current.space()
		if current.ended() {
			return fields
		}
		if current.peek() != '"' {
			if !current.skip() {
				return fields
			}
			continue
		}
		value, complete := current.string()
		if !complete {
			fields.Streaming, fields.Partial = key, value
			return fields
		}
		fields.Complete[key] = value
	}
}

type reader struct {
	text     string
	position int
}

func (current *reader) ended() bool {
	return current.position >= len(current.text)
}

func (current *reader) peek() byte {
	return current.text[current.position]
}

func (current *reader) take(wanted byte) bool {
	if current.ended() || current.peek() != wanted {
		return false
	}
	current.position++
	return true
}

func (current *reader) space() {
	for !current.ended() && strings.IndexByte(" \t\r\n", current.peek()) >= 0 {
		current.position++
	}
}

// string reads the string starting at the opening quote. It returns what it
// decoded and whether the closing quote arrived; when it did not, the
// decoded text stops before any escape or character still incomplete.
func (current *reader) string() (string, bool) {
	current.position++
	var decoded strings.Builder
	for !current.ended() {
		switch character := current.peek(); {
		case character == '"':
			current.position++
			return decoded.String(), true
		case character == '\\':
			text, width := escape(current.text[current.position:])
			if width == 0 {
				return decoded.String(), false
			}
			decoded.WriteString(text)
			current.position += width
		case character < utf8.RuneSelf:
			decoded.WriteByte(character)
			current.position++
		default:
			rest := current.text[current.position:]
			if !utf8.FullRuneInString(rest) {
				return decoded.String(), false
			}
			_, width := utf8.DecodeRuneInString(rest)
			decoded.WriteString(rest[:width])
			current.position += width
		}
	}
	return decoded.String(), false
}

var simple_escapes = map[byte]string{'"': `"`, '\\': `\`, '/': "/", 'b': "\b", 'f': "\f", 'n': "\n", 'r': "\r", 't': "\t"}

// escape decodes the escape at the start of text, returning the text it
// stands for and its width, or width 0 when the escape is still incomplete.
func escape(text string) (string, int) {
	if len(text) < 2 {
		return "", 0
	}
	if replacement, ok := simple_escapes[text[1]]; ok {
		return replacement, 2
	}
	if text[1] != 'u' {
		return string(utf8.RuneError), 2
	}
	first, ok := hex(text[2:])
	if !ok {
		return "", 0
	}
	if !utf16.IsSurrogate(first) {
		return string(first), 6
	}
	if len(text) < 7 {
		return "", 0
	}
	if text[6] != '\\' {
		return string(utf8.RuneError), 6
	}
	if len(text) < 8 {
		return "", 0
	}
	if text[7] != 'u' {
		return string(utf8.RuneError), 6
	}
	second, ok := hex(text[8:])
	if !ok {
		return "", 0
	}
	if pair := utf16.DecodeRune(first, second); pair != utf8.RuneError {
		return string(pair), 12
	}
	return string(utf8.RuneError), 6
}

func hex(text string) (rune, bool) {
	if len(text) < 4 {
		return 0, false
	}
	value, error := strconv.ParseUint(text[:4], 16, 16)
	if error != nil {
		return utf8.RuneError, true
	}
	return rune(value), true
}

// skip passes over a value that is not a string, reporting whether all of
// it has arrived.
func (current *reader) skip() bool {
	depth := 0
	for !current.ended() {
		switch character := current.peek(); character {
		case '"':
			if _, complete := current.string(); !complete {
				return false
			}
			if depth == 0 {
				return true
			}
			continue
		case '{', '[':
			depth++
		case '}', ']':
			if depth == 0 {
				return true
			}
			depth--
			if depth == 0 {
				current.position++
				return true
			}
		case ',', ' ', '\t', '\r', '\n':
			if depth == 0 {
				return true
			}
		}
		current.position++
	}
	return false
}
