package edit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

type Header struct {
	ID      string    `json:"id"`
	Label   string    `json:"label"`
	Created time.Time `json:"created"`
	State   string    `json:"state"`
}

// Inspect reads only the bounded header of each journal. It neither creates a
// directory/lock nor decodes binary backups just to list repair/undo status.
func (store Store) Inspect() ([]Header, error) {
	info, err := os.Lstat(store.Dir)
	if os.IsNotExist(err) {
		return []Header{}, nil
	}
	if err != nil {
		return nil, err
	}
	stat, owned := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 || !owned || int(stat.Uid) != os.Getuid() {
		return nil, errors.New("journal directory must be a private user-owned directory")
	}
	entries, err := os.ReadDir(store.Dir)
	if err != nil {
		return nil, err
	}
	headers := []Header{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		file, err := os.OpenFile(filepath.Join(store.Dir, entry.Name()), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return nil, err
		}
		header, err := inspectHeader(file)
		file.Close()
		if err != nil {
			return nil, fmt.Errorf("read journal header %s: %w", entry.Name(), err)
		}
		if header.ID+".json" != entry.Name() {
			return nil, errors.New("journal filename/header ID mismatch")
		}
		headers = append(headers, header)
	}
	sort.Slice(headers, func(i, j int) bool { return headers[i].Created.After(headers[j].Created) })
	return headers, nil
}

func inspectHeader(reader io.Reader) (Header, error) {
	decoder := json.NewDecoder(io.LimitReader(reader, 16<<10))
	var header Header
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return header, errors.New("invalid journal object")
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return header, err
		}
		var target any
		switch token {
		case "id":
			target = &header.ID
		case "label":
			target = &header.Label
		case "created":
			target = &header.Created
		case "state":
			target = &header.State
		default:
			return header, errors.New("journal metadata missing or reordered")
		}
		if err := decoder.Decode(target); err != nil {
			return header, err
		}
		if token == "state" {
			if header.ID == "" || (header.State != "prepared" && header.State != "committed" && header.State != "undoing" && header.State != "undone") {
				return header, errors.New("invalid journal state")
			}
			return header, nil
		}
	}
	return header, io.ErrUnexpectedEOF
}
