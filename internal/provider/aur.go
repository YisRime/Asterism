package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"slices"
	"strings"

	asterism "asterism/internal"
)

// 解析归档
func ParseAUR() ([]*asterism.Package, error) {
	raw, err := asterism.Fetch("https://aur.archlinux.org/packages-meta-ext-v1.json.gz", "", nil, true)
	if err != nil {
		return nil, err
	}
	text := asterism.Unzip(raw)

	var out []*asterism.Package
	scanner := bufio.NewScanner(bytes.NewReader(text))
	scanner.Buffer(make([]byte, 0, 1<<20), 8<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "[" || line == "]" || line == "[]" {
			continue
		}
		line = strings.TrimSuffix(line, ",")
		if line == "" {
			continue
		}
		var entry struct {
			Name           string   `json:"Name"`
			PackageBase    string   `json:"PackageBase"`
			Description    string   `json:"Description"`
			URL            string   `json:"URL"`
			URLPath        string   `json:"URLPath"`
			Version        string   `json:"Version"`
			License        []string `json:"License"`
			Maintainer     string   `json:"Maintainer"`
			CoMaintainers  []string `json:"CoMaintainers"`
			FirstSubmitted int64    `json:"FirstSubmitted"`
			LastModified   int64    `json:"LastModified"`
			Expired        int64    `json:"OutOfDate"`
			VoteCount      int      `json:"NumVotes"`
			Popularity     float64  `json:"Popularity"`
			Depends        []string `json:"Depends"`
			MakeDepends    []string `json:"MakeDepends"`
			OptDepends     []string `json:"OptDepends"`
			Conflicts      []string `json:"Conflicts"`
			Provides       []string `json:"Provides"`
			Groups         []string `json:"Groups"`
			Keywords       []string `json:"Keywords"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Name == "" || excludedPackage(entry.Name, []string{
			"python-", "python2-", "perl-", "ruby-", "nodejs-", "php-", "haskell-",
			"mingw-w64-", "lib32-", "r-", "texlive-", "fonts-", "ttf-", "otf-",
			"gcc-", "clang-", "llvm-", "ocaml-", "erlang-", "lua-", "java-",
			"sddm-", "grub-", "plymouth-", "icu", "glibc-", "kernel-",
		}, []string{
			"-doc", "-docs", "-debug", "-headers", "-static", "-devel", "-libs",
			"-src", "-git", "-svn", "-hg", "-bzr", "-nightly", "-bin", "-beta",
			"-alpha", "-rc", "-language-pack",
		}, false) {
			continue
		}
		licenseKey := ""
		for _, candidate := range entry.License {
			if licenseKey = asterism.NormalizeLicense(candidate); licenseKey != "" {
				break
			}
		}
		base := entry.PackageBase
		if base == "" {
			base = entry.Name
		}
		cleaned := base
		lower := strings.ToLower(cleaned)
		suffixes := []string{
			"-appimage-extract-and-run", "-appimage", "-xdg", "-wayland", "-x11",
			"-portable", "-installer",
		}
		if index := slices.IndexFunc(suffixes, func(suffix string) bool {
			return strings.HasSuffix(lower, suffix) && len(cleaned) > len(suffix)+1
		}); index >= 0 {
			cleaned = cleaned[:len(cleaned)-len(suffixes[index])]
		}
		maintainers := []string{}
		if entry.Maintainer != "" {
			maintainers = append(maintainers, entry.Maintainer)
		}
		maintainers = append(maintainers, entry.CoMaintainers...)
		link := map[string]string{}
		if entry.URL != "" {
			link["homepage"] = entry.URL
		}
		if entry.URLPath != "" {
			link["download"] = "https://aur.archlinux.org" + entry.URLPath
		}
		record := &asterism.Package{
			Source: "aur", Origin: entry.Name, Name: cleaned,
			Summary: entry.Description, Repository: asterism.NormalizeRepository(entry.URL),
			License: licenseKey, Author: entry.Maintainer, Maintainer: maintainers,
			Version: entry.Version, Change: asterism.UnixTime(entry.LastModified),
			Vote: entry.VoteCount, Popularity: entry.Popularity,
			Depend: append(append(append([]string{}, entry.Depends...), entry.MakeDepends...), entry.OptDepends...),
			Label: map[string][]string{
				"keyword": entry.Keywords,
				"group":   entry.Groups,
			},
			Platform: []string{"Linux"}, Link: link,
		}
		if entry.Expired > 0 {
			record.Label["status"] = []string{"outdated"}
		}
		out = append(out, record)
	}
	return out, nil
}
