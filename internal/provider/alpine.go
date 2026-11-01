package provider

import (
	"bufio"
	"bytes"
	"io"
	"strconv"
	"strings"

	asterism "asterism/internal"
)

// 解析索引
func parseIndex(raw []byte, repo string) []*asterism.Package {
	var out []*asterism.Package
	fields := map[string]string{}

	flush := func() {
		name := fields["P"]
		if name != "" && !excludedPackage(name, []string{
			"py3-", "py-", "perl-", "ruby-", "php-", "node-", "lua-", "r-",
			"font-", "fonts-", "texlive-", "linux-", "firmware-",
		}, []string{
			"-dev", "-doc", "-docs", "-dbg", "-static", "-lang", "-common",
			"-headers", "-locale", "-l10n", "-data", "-dbgsym",
		}, true) {
			published := ""
			if stamp, err := strconv.ParseInt(strings.TrimSpace(fields["t"]), 10, 64); err == nil {
				published = asterism.UnixTime(stamp)
			}
			link := map[string]string{}
			if fields["U"] != "" {
				link["homepage"] = fields["U"]
			}
			record := &asterism.Package{
				Source: "alpine", Origin: name, Name: name,
				Summary: fields["T"], Repository: asterism.NormalizeRepository(fields["U"]),
				License: asterism.NormalizeLicense(fields["L"]),
				Author:  fields["m"], Version: fields["V"], Release: published,
				Depend: strings.Fields(fields["D"]), Platform: []string{"Linux"},
				Label: map[string][]string{"repo": {repo}}, Link: link,
			}
			if size, err := strconv.ParseInt(strings.TrimSpace(fields["S"]), 10, 64); err == nil {
				record.Size, record.Unit = size, "package"
			}
			hashes := map[string]string{}
			if fields["c"] != "" {
				hashes["commit"] = fields["c"]
			}
			if len(hashes) > 0 {
				record.Hash = hashes
			}
			if fields["p"] != "" {
				record.Label["provides"] = strings.Fields(fields["p"])
			}
			out = append(out, record)
		}
		fields = map[string]string{}
	}

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		index := strings.IndexByte(line, ':')
		if index <= 0 {
			continue
		}
		fields[line[:index]] = strings.TrimSpace(line[index+1:])
	}
	flush()
	return out
}

// 解析发行
func ParseAlpine() ([]*asterism.Package, error) {
	return collectRepos([]repoItem{
		{"main", "https://dl-cdn.alpinelinux.org/alpine/latest-stable/main/x86_64/APKINDEX.tar.gz"},
		{"community", "https://dl-cdn.alpinelinux.org/alpine/latest-stable/community/x86_64/APKINDEX.tar.gz"},
	}, func(address, repo string) ([]*asterism.Package, error) {
		raw, err := asterism.Fetch(address, "", nil, true)
		if err != nil {
			return nil, err
		}
		found := false
		var records []*asterism.Package
		err = asterism.WalkArchive(raw, func(name string, size int64, reader io.Reader) error {
			if found || !strings.HasSuffix(name, "APKINDEX") {
				return nil
			}
			content, err := io.ReadAll(reader)
			if err != nil {
				return err
			}
			records = parseIndex(content, repo)
			found = true
			return nil
		})
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, asterism.ErrFailed
		}
		return records, nil
	})
}
