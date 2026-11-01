package provider

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"io"
	"os"
	"strings"

	_ "modernc.org/sqlite"

	asterism "asterism/internal"
)

// 解析索引
func ParseWinget() ([]*asterism.Package, error) {
	raw, err := asterism.Fetch("https://cdn.winget.microsoft.com/cache/source.msix", "", nil, true)
	if err != nil {
		return nil, err
	}
	return parseBundle(raw)
}

// 解析封装
func parseBundle(raw []byte) ([]*asterism.Package, error) {
	database, err := wingetDatabase(raw)
	if err != nil {
		return nil, err
	}

	file, err := os.CreateTemp("", "asterism-winget-*.db")
	if err != nil {
		return nil, err
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.Write(database); err != nil {
		file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}

	store, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	rows, err := store.Query(`SELECT i.id, n.name, v.version
		FROM manifest m
		JOIN ids i ON i.rowid = m.id
		JOIN names n ON n.rowid = m.name
		JOIN versions v ON v.rowid = m.version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type bestEntry struct {
		version string
		name    string
	}
	best := make(map[string]bestEntry, 16384)
	for rows.Next() {
		var id, name, version string
		if err := rows.Scan(&id, &name, &version); err != nil {
			return nil, err
		}
		if id == "" {
			continue
		}
		if current, ok := best[id]; !ok || compareVersion(version, current.version) > 0 {
			best[id] = bestEntry{version: version, name: name}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]*asterism.Package, 0, len(best))
	for id, item := range best {
		name := item.name
		if name == "" {
			name = id
		}
		author := id
		if index := strings.IndexByte(id, '.'); index >= 0 {
			author = id[:index]
		}
		var aliases []string
		if index := strings.LastIndexByte(id, '.'); index >= 0 && index+1 < len(id) {
			aliases = []string{id[index+1:]}
		}
		out = append(out, &asterism.Package{
			Source: "winget", Origin: id, Name: name,
			Author: author, Version: item.version,
			Platform: []string{"Windows"}, Alias: aliases,
		})
	}
	return out, nil
}

// 提取库文
func wingetDatabase(raw []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, err
	}
	var entry *zip.File
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := strings.ToLower(file.Name)
		if name == "public/index.db" {
			entry = file
			break
		}
		if entry == nil && strings.HasSuffix(name, "index.db") {
			entry = file
		}
	}
	if entry == nil {
		return nil, asterism.ErrFailed
	}
	stream, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	database, err := io.ReadAll(stream)
	if err != nil {
		return nil, err
	}
	return database, nil
}

// 比较版本
func compareVersion(left, right string) int {
	leftOffset, rightOffset := 0, 0
	for leftOffset < len(left) && rightOffset < len(right) {
		leftToken, leftNumeric, leftNext := versionToken(left, leftOffset)
		rightToken, rightNumeric, rightNext := versionToken(right, rightOffset)

		var compare int
		switch {
		case leftNumeric && rightNumeric:
			leftDigits := strings.TrimLeft(leftToken, "0")
			rightDigits := strings.TrimLeft(rightToken, "0")
			if len(leftDigits) < len(rightDigits) {
				compare = -1
			} else if len(leftDigits) > len(rightDigits) {
				compare = 1
			} else {
				compare = strings.Compare(leftDigits, rightDigits)
			}
		case !leftNumeric && !rightNumeric:
			compare = strings.Compare(leftToken, rightToken)
		case leftNumeric:
			compare = 1
		default:
			compare = -1
		}
		if compare != 0 {
			return compare
		}
		leftOffset, rightOffset = leftNext, rightNext
	}
	switch {
	case leftOffset < len(left):
		return 1
	case rightOffset < len(right):
		return -1
	}
	return 0
}

// 提取片段
func versionToken(text string, start int) (token string, numeric bool, next int) {
	isDigit := func(char byte) bool { return char >= '0' && char <= '9' }
	digit := isDigit(text[start])
	end := start + 1
	for end < len(text) && isDigit(text[end]) == digit {
		end++
	}
	return text[start:end], digit, end
}
