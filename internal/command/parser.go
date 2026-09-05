// Package command parses only the supported commit-message subset without ever
// evaluating shell syntax.
package command

import (
	"strings"
	"unicode"

	"github.com/gongahkia/zsh-git-inlay/internal/candidate"
)

type Message struct {
	Prefix string
	Quote  byte // zero is unquoted; a non-zero quote remains open in the buffer.
}

// Parse returns a message position only when BUFFER ends in the current -m
// argument. Shell operators, substitutions and completed arguments are safely
// rejected rather than interpreted.
func Parse(buffer string, cursor int) (Message, bool) {
	if cursor != len(buffer) || len(buffer) > 4096 || strings.ContainsAny(buffer, "\x00\n\r;|&`$<>") {
		return Message{}, false
	}
	words, malformed := lex(buffer)
	if malformed || len(words) < 3 {
		return Message{}, false
	}
	position := 0
	if words[0].value == "command" {
		position++
		if len(words) < 4 {
			return Message{}, false
		}
	}
	if words[position].quoted || words[position].value != "git" || words[position+1].quoted || words[position+1].value != "commit" {
		return Message{}, false
	}
	position += 2
	for position < len(words) {
		word := words[position]
		if forbidden(word.value) {
			return Message{}, false
		}
		if word.value == "-m" || word.value == "--message" {
			if position+1 == len(words) {
				return Message{}, true
			}
			argument := words[position+1]
			if position+2 != len(words) || argument.closed {
				return Message{}, false
			}
			return Message{Prefix: argument.value, Quote: argument.quote}, true
		}
		if strings.HasPrefix(word.value, "--message=") {
			if position+1 != len(words) || word.closed {
				return Message{}, false
			}
			return Message{Prefix: strings.TrimPrefix(word.value, "--message="), Quote: word.quote}, true
		}
		if shortMessage(word.value) {
			if position+1 == len(words) {
				return Message{}, true
			}
			argument := words[position+1]
			if position+2 != len(words) || argument.closed {
				return Message{}, false
			}
			return Message{Prefix: argument.value, Quote: argument.quote}, true
		}
		position++
	}
	return Message{}, false
}

type word struct {
	value          string
	quote          byte
	quoted, closed bool
}

func lex(input string) ([]word, bool) {
	words := []word{}
	for position := 0; position < len(input); {
		for position < len(input) && unicode.IsSpace(rune(input[position])) {
			position++
		}
		if position == len(input) {
			break
		}
		current := word{}
		if input[position] == '\'' || input[position] == '"' {
			current.quote, current.quoted = input[position], true
			position++
			start := position
			for position < len(input) && input[position] != current.quote {
				position++
			}
			current.value = input[start:position]
			if position < len(input) {
				current.closed = true
				position++
			}
			if position < len(input) && !unicode.IsSpace(rune(input[position])) {
				return nil, true
			}
		} else {
			start := position
			for position < len(input) && !unicode.IsSpace(rune(input[position])) {
				position++
			}
			current.value = input[start:position]
		}
		words = append(words, current)
	}
	return words, false
}

func forbidden(value string) bool {
	return value == "--amend" || strings.HasPrefix(value, "--fixup") || strings.HasPrefix(value, "--squash")
}

func shortMessage(value string) bool {
	if !strings.HasPrefix(value, "-") || strings.HasPrefix(value, "--") || len(value) < 2 {
		return false
	}
	for _, option := range value[1:] {
		if option == 'm' {
			return true
		}
	}
	return false
}

// Suggestion returns a full autosuggestion, which must begin with buffer.
func Suggestion(buffer string, cursor int, candidates []candidate.Candidate, cycle int) (string, bool) {
	message, ok := Parse(buffer, cursor)
	if !ok || len(candidates) == 0 {
		return "", false
	}
	if cycle < 0 {
		cycle = 0
	}
	chosen := candidates[cycle%len(candidates)].Message
	if !strings.HasPrefix(chosen, message.Prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(chosen, message.Prefix)
	switch message.Quote {
	case '\'':
		return buffer + rest + "'", true
	case '"':
		return buffer + rest + "\"", true
	case 0:
		if message.Prefix == "" {
			return buffer + "'" + chosen + "'", true
		}
		return buffer + escapeUnquoted(rest), true
	default:
		return "", false
	}
}

func escapeUnquoted(value string) string {
	var builder strings.Builder
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || strings.ContainsRune("._/-", character) {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('\\')
			builder.WriteRune(character)
		}
	}
	return builder.String()
}
