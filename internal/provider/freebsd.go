package provider

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"

	"github.com/klauspost/compress/zstd"

	asterism "asterism/internal"
)

// 版本尾巴
var versionTail = regexp.MustCompile(`-[0-9][0-9._,+_]*$`)

// 版本前缀
var versionPrefix = regexp.MustCompile(`^(?:py[0-9]+|php[0-9]+|ruby[0-9]+|node[0-9]+|lua[0-9]+)-`)

// 解析清单
func ParseFreeBSD() ([]*asterism.Package, error) {
	raw, err := asterism.Fetch("https://download.freebsd.org/ftp/ports/index/INDEX-15.zst", "", nil, true)
	if err != nil {
		return nil, err
	}
	reader, err := zstd.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var out []*asterism.Package
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) < 10 {
			continue
		}
		origin := strings.TrimPrefix(fields[1], "/usr/ports/")
		if origin == "" || !strings.Contains(origin, "/") {
			continue
		}
		port := origin[strings.LastIndexByte(origin, '/')+1:]
		if port == "" || !portName(port) {
			continue
		}
		version := strings.TrimPrefix(fields[0], port+"-")
		if version == fields[0] {
			continue
		}

		var dependencies []string
		for _, item := range strings.Fields(fields[8]) {
			name := versionTail.ReplaceAllString(item, "")
			if name != "" && name != port {
				dependencies = append(dependencies, name)
			}
		}

		homepage := fields[9]
		link := map[string]string{}
		if homepage != "" {
			link["homepage"] = homepage
		}
		record := &asterism.Package{
			Source: "freebsd", Origin: origin, Name: port,
			Summary: fields[3], Description: fields[3],
			Repository: asterism.NormalizeRepository(homepage),
			Author:     fields[5], Version: version, Depend: dependencies,
			Label:    map[string][]string{"category": strings.Fields(fields[6])},
			Platform: []string{"FreeBSD"}, Link: link,
		}
		out = append(out, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, asterism.ErrFailed
	}
	return out, nil
}

// 包名过滤
func portName(name string) bool {
	if versionPrefix.MatchString(name) {
		return false
	}
	return !excludedPackage(name, []string{
		"lib", "p5-", "pecl-", "pear-", "r-cran-", "erlang-", "elixir-", "rubygem-",
		"gcc", "llvm", "clang", "gstreamer1", "texlive", "tex-", "linux-", "xorg-",
		"qt5-", "qt6-", "gtk2", "gtk3", "gtk4", "luajit", "font-", "gettext",
	}, []string{
		"-devel", "-doc", "-docs", "-manual", "-reference", "-data", "-nls",
		"-l10n", "-locale", "-datadir",
	}, true)
}
