package provider

import (
	"bufio"
	"bytes"
	"slices"
	"strconv"
	"strings"

	asterism "asterism/internal"
)

// 截取分类
func debianSection(raw string) string {
	if index := strings.IndexByte(raw, '/'); index >= 0 {
		return strings.TrimSpace(raw[:index])
	}
	return strings.TrimSpace(raw)
}

// 解析索引
func parsePackages(text string, popcon map[string]int64) []*asterism.Package {
	var out []*asterism.Package
	fields := make(map[string]string, 16)
	var summary string
	var long strings.Builder
	var hasDescription bool

	flush := func() {
		name := fields["Package"]
		if name != "" && slices.Contains([]string{
			"utils", "net", "admin", "science", "text", "x11", "sound", "games",
			"misc", "graphics", "web", "gnome", "math", "mail", "editors",
			"database", "kde", "video", "electronics", "hamradio", "comm",
			"education", "office", "news", "shells", "otherosfs", "print", "zope",
		}, debianSection(fields["Section"])) &&
			!excludedPackage(name, []string{
				"lib", "python", "python3", "perl", "ruby", "php", "golang", "gcc",
				"fonts-", "texlive", "linux-", "firmware-", "node-", "rust-", "cargo-",
				"haskell-", "ocaml-", "r-", "lua-", "vim-", "emacs-",
			}, []string{
				"-dev", "-dbg", "-doc", "-data", "-common", "-dbgsym", "-locale",
				"-l10n", "-headers", "-modules", "-kmods",
			}, true) {
			homepage := fields["Homepage"]
			var dependencies []string
			for _, value := range []string{fields["Pre-Depends"], fields["Depends"], fields["Recommends"], fields["Suggests"]} {
				for _, item := range strings.Split(value, ",") {
					item = strings.TrimSpace(strings.SplitN(item, "(", 2)[0])
					item = strings.TrimSpace(strings.SplitN(item, ":", 2)[0])
					if item != "" {
						dependencies = append(dependencies, item)
					}
				}
			}
			links := map[string]string{}
			if homepage != "" {
				links["homepage"] = homepage
			}
			if fields["Filename"] != "" {
				links["download"] = "https://deb.debian.org/debian/" + fields["Filename"]
			}
			if fields["Bugs"] != "" {
				links["bugs"] = fields["Bugs"]
			}
			label := map[string][]string{
				"category": {debianSection(fields["Section"])},
				"keyword":  strings.Fields(fields["Tags"]),
			}
			if fields["Architecture"] != "" {
				label["arch"] = []string{fields["Architecture"]}
			}
			if fields["Priority"] != "" {
				label["priority"] = []string{fields["Priority"]}
			}
			if fields["Origin"] != "" {
				label["vendor"] = []string{fields["Origin"]}
			}
			record := &asterism.Package{
				Source: "debian", Origin: name, Name: name,
				Summary: summary, Repository: asterism.NormalizeRepository(homepage),
				Description: strings.TrimSpace(summary + "\n" + long.String()),
				Author:      fields["Maintainer"], Version: fields["Version"],
				Download: popcon[name], Window: "survey",
				Depend: dependencies, Label: label,
				Platform: []string{"Linux"}, Link: links,
			}
			if size, err := strconv.ParseInt(strings.TrimSpace(fields["Size"]), 10, 64); err == nil {
				record.Size, record.Unit = size, "package"
			}
			hashes := map[string]string{}
			for _, item := range []struct{ key, value string }{
				{"md5", fields["MD5sum"]},
				{"sha256", fields["SHA256"]},
			} {
				if item.value != "" {
					hashes[item.key] = item.value
				}
			}
			if len(hashes) > 0 {
				record.Hash = hashes
			}
			out = append(out, record)
		}
		fields = make(map[string]string, 16)
		summary, hasDescription = "", false
		long.Reset()
	}

	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if hasDescription {
				long.WriteString(strings.TrimSpace(line))
				long.WriteByte('\n')
			}
			continue
		}
		index := strings.IndexByte(line, ':')
		if index <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:index])
		value := strings.TrimSpace(line[index+1:])
		switch key {
		case "Package", "Version", "Homepage", "Maintainer", "Section",
			"Architecture", "Priority", "Installed-Size", "Size", "Filename",
			"MD5sum", "SHA256", "Pre-Depends", "Depends", "Recommends",
			"Suggests", "Tags", "Bugs", "Origin":
			fields[key] = value
		case "Description":
			hasDescription = true
			summary = value
		}
	}
	flush()

	return out
}

// 解析发行
func ParseDebian() ([]*asterism.Package, error) {
	raw, err := asterism.Fetch("https://deb.debian.org/debian/dists/stable/main/binary-amd64/Packages.gz", "", nil, true)
	if err != nil {
		return nil, err
	}
	text := asterism.Unzip(raw)
	if len(text) == 0 {
		return nil, asterism.ErrFailed
	}

	total := bytes.Count(text, []byte("\nPackage:"))
	if bytes.HasPrefix(text, []byte("Package:")) {
		total++
	}
	if total == 0 {
		return nil, asterism.ErrFailed
	}

	statistics, _ := asterism.Fetch("https://popcon.debian.org/by_inst.gz", "", nil, true)
	popcon := make(map[string]int64)
	popconScanner := bufio.NewScanner(bytes.NewReader(asterism.Unzip(statistics)))
	popconScanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for popconScanner.Scan() {
		line := strings.TrimSpace(popconScanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		count, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			continue
		}
		popcon[fields[1]] = count
	}

	records := parsePackages(string(text), popcon)

	return records, nil
}
