package lifecycle

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/gongahkia/zsh-git-inlay/internal/edit"
	"github.com/gongahkia/zsh-git-inlay/internal/setup"
)

const (
	BlockStart = "# >>> zsh-git-inlay managed >>>"
	BlockEnd   = "# <<< zsh-git-inlay managed <<<"
)

var literalSource = regexp.MustCompile("^\\s*(source|\\.)\\s+('[^'\\n]*'|\"[^\"$`\\n]*\"|[^ \\t;|&<>$`\\n]+)\\s*(#.*)?$")

func Block(prefix, dependency string) []byte {
	return []byte(fmt.Sprintf("%s\npath=(%s $path)\nif (( ! ${+functions[_zsh_autosuggest_fetch_suggestion]} )); then\n  source %s\nfi\nsource %s\n%s\n", BlockStart, setup.Quote(prefix+"/bin"), setup.Quote(dependency), setup.Quote(prefix+"/share/zsh-git-inlay/zsh-git-inlay.plugin.zsh"), BlockEnd))
}

func region(content []byte) (int, int, error) {
	start, end, offset := -1, -1, 0
	for _, raw := range bytes.SplitAfter(content, []byte("\n")) {
		line := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
		switch line {
		case BlockStart:
			if start >= 0 || end >= 0 {
				return 0, 0, errors.New("multiple or nested zsh-git-inlay managed blocks; inspect the shell file before editing")
			}
			start = offset
		case BlockEnd:
			if start < 0 || end >= 0 {
				return 0, 0, errors.New("unmatched zsh-git-inlay block end; inspect the shell file before editing")
			}
			end = offset + len(raw)
		}
		offset += len(raw)
	}
	if start >= 0 && end < 0 {
		return 0, 0, errors.New("unterminated zsh-git-inlay managed block; inspect the shell file before editing")
	}
	return start, end, nil
}

// ReplaceBlock preserves every byte outside the one owned region. Legacy
// source lines are migrated only with the caller's explicit migrate option.
func ReplaceBlock(content, block []byte, migrate bool) ([]byte, error) {
	start, end, err := region(content)
	if err != nil {
		return nil, err
	}
	outside := content
	if start >= 0 {
		outside = append(append([]byte(nil), content[:start]...), content[end:]...)
	}
	for _, line := range strings.Split(string(outside), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || !strings.Contains(line, "zsh-git-inlay.plugin.zsh") {
			continue
		}
		if !migrate || !literalSource.MatchString(line) {
			return nil, fmt.Errorf("unmanaged integration outside the marked block: %s; remove it or migrate a literal source line explicitly", trimmed)
		}
		outside = bytes.Replace(outside, []byte(line), []byte("# zsh-git-inlay migrated: "+line), 1)
	}
	if migrate && !bytes.Equal(outside, content) {
		// Migration keeps the old command as a comment and appends one
		// canonical block. Its exact original bytes remain in the journal.
		content, start, end = outside, -1, -1
	}
	if start >= 0 {
		return append(append(append([]byte(nil), content[:start]...), block...), content[end:]...), nil
	}
	if len(block) == 0 {
		return append([]byte(nil), content...), nil
	}
	result := append([]byte(nil), content...)
	if len(result) > 0 && result[len(result)-1] != '\n' {
		result = append(result, '\n')
	}
	return append(result, block...), nil
}

func RemoveBlock(content []byte) ([]byte, error) {
	start, end, err := region(content)
	if err != nil || start < 0 {
		return content, err
	}
	return append(append([]byte(nil), content[:start]...), content[end:]...), nil
}

// MergeUndo is intentionally narrow: only an intact owned block can be merged
// into a shell file with later unrelated edits. Binary/config changes conflict.
func MergeUndo(change edit.Change, current edit.Snapshot) (edit.Change, error) {
	if !change.ManagedBlock || current.Target != change.Before.Target {
		return edit.Change{}, fmt.Errorf("%s changed after the transaction", current.Path)
	}
	beforeOutside, beforeErr := RemoveBlock(change.Before.Data)
	afterOutside, afterErr := RemoveBlock(change.Data)
	if beforeErr != nil || afterErr != nil || !bytes.Equal(bytes.TrimSuffix(beforeOutside, []byte("\n")), bytes.TrimSuffix(afterOutside, []byte("\n"))) {
		return edit.Change{}, fmt.Errorf("%s also changed outside the owned block; reconcile its original backup before undo", current.Path)
	}
	start, end, err := region(change.Data)
	if err != nil || change.Remove {
		return edit.Change{}, fmt.Errorf("%s changed after the transaction", current.Path)
	}
	currentStart, currentEnd, err := region(current.Data)
	if err != nil {
		return edit.Change{}, err
	}
	if (start < 0 && currentStart >= 0) || (start >= 0 && (currentStart < 0 || !bytes.Equal(current.Data[currentStart:currentEnd], change.Data[start:end]))) {
		return edit.Change{}, fmt.Errorf("managed region or target in %s changed after the transaction", current.Path)
	}
	beforeStart, beforeEnd, err := region(change.Before.Data)
	if err != nil {
		return edit.Change{}, err
	}
	var previous []byte
	if beforeStart >= 0 {
		previous = change.Before.Data[beforeStart:beforeEnd]
	}
	if start < 0 {
		merged, err := ReplaceBlock(current.Data, previous, false)
		if err != nil {
			return edit.Change{}, err
		}
		return edit.Change{Before: current, Data: merged, Mode: current.Mode}, nil
	}
	merged := append(append(append([]byte(nil), current.Data[:currentStart]...), previous...), current.Data[currentEnd:]...)
	return edit.Change{Before: current, Data: merged, Mode: current.Mode}, nil
}
