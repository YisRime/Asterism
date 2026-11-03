package provider

import (
	"io"
	"path"
	"strconv"
	"strings"

	asterism "asterism/internal"
)

// 解析描述
func parseDesc(content []byte, repo string) *asterism.Package {
	fields := map[string]string{}
	key := ""
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "%") && strings.HasSuffix(line, "%") {
			key = line
			continue
		}
		if key == "" {
			continue
		}
		if fields[key] != "" {
			fields[key] += "\n"
		}
		fields[key] += line
	}

	name := fields["%NAME%"]
	if name == "" {
		return nil
	}
	published := ""
	if stamp, err := strconv.ParseInt(strings.TrimSpace(fields["%BUILDDATE%"]), 10, 64); err == nil {
		published = asterism.UnixTime(stamp)
	}
	license := ""
	normalized := strings.ReplaceAll(fields["%LICENSE%"], "\n", " AND ")
	normalized = strings.ReplaceAll(normalized, ",", " AND ")
	normalized = strings.ReplaceAll(normalized, " OR ", " AND ")
	for _, part := range strings.Split(normalized, " AND ") {
		if id := asterism.NormalizeLicense(strings.TrimSpace(part)); id != "" {
			license = id
			break
		}
	}
	link := map[string]string{}
	if fields["%URL%"] != "" {
		link["homepage"] = fields["%URL%"]
	}
	var optdepends []string
	for _, line := range strings.Split(fields["%OPTDEPENDS%"], "\n") {
		if index := strings.IndexByte(line, ':'); index > 0 {
			optdepends = append(optdepends, line[:index])
		}
	}
	record := &asterism.Package{
		Source: "arch", Origin: name, Name: name,
		Summary: fields["%DESC%"], Repository: asterism.NormalizeRepository(fields["%URL%"]),
		License: license, Author: fields["%PACKAGER%"], Version: fields["%VERSION%"],
		Release: published,
		Label: map[string][]string{
			"group": strings.Fields(fields["%GROUPS%"]),
			"repo":  {repo},
		},
		Depend:   append(strings.Fields(fields["%DEPENDS%"]), optdepends...),
		Platform: []string{"Linux"}, Link: link,
	}
	if base := fields["%BASE%"]; base != "" && base != name {
		record.Label["base"] = []string{base}
	}
	if size, err := strconv.ParseInt(strings.TrimSpace(fields["%CSIZE%"]), 10, 64); err == nil {
		record.Size, record.Unit = size, "package"
	} else if size, err := strconv.ParseInt(strings.TrimSpace(fields["%ISIZE%"]), 10, 64); err == nil {
		record.Size, record.Unit = size, "install"
	}
	hashes := map[string]string{}
	for _, item := range []struct{ key, value string }{
		{"md5", fields["%MD5SUM%"]},
		{"sha256", fields["%SHA256SUM%"]},
		{"pgp", fields["%PGPSIG%"]},
	} {
		if item.value != "" {
			hashes[item.key] = item.value
		}
	}
	if len(hashes) > 0 {
		record.Hash = hashes
	}
	return record
}

// 解析发行
func ParseArch() ([]*asterism.Package, error) {
	return collectRepos([]repoItem{
		{"core", "https://geo.mirror.pkgbuild.com/core/os/x86_64/core.db"},
		{"extra", "https://geo.mirror.pkgbuild.com/extra/os/x86_64/extra.db"},
		{"multilib", "https://geo.mirror.pkgbuild.com/multilib/os/x86_64/multilib.db"},
	}, func(address, repo string) ([]*asterism.Package, error) {
		raw, err := asterism.Fetch(address, "", nil, true)
		if err != nil {
			return nil, err
		}
		var out []*asterism.Package
		err = asterism.WalkArchive(raw, func(name string, size int64, reader io.Reader) error {
			if path.Base(name) != "desc" {
				return nil
			}
			content, err := io.ReadAll(reader)
			if err != nil {
				return err
			}
			if record := parseDesc(content, repo); record != nil {
				out = append(out, record)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return out, nil
	})
}
