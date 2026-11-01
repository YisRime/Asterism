package provider

import (
	"bytes"
	"encoding/xml"
	"io"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"

	asterism "asterism/internal"
)

// 走镜像站
const mirrorBase = "https://ftp.gwdg.de/pub/opensuse/tumbleweed/repo/oss/"

// 解析发行
func ParseOpenSUSE() ([]*asterism.Package, error) {
	// 指针易变
	index, err := asterism.Fetch(mirrorBase+"repodata/repomd.xml", "", nil, false)
	if err != nil {
		return nil, err
	}
	root, err := asterism.Decode(index)
	if err != nil {
		return nil, err
	}
	location := ""
	for _, data := range root.Children("data") {
		if data.Attribute("type") == "primary" {
			if node := data.Child("location"); node != nil {
				location = node.Attribute("href")
			}
			break
		}
	}
	if location == "" {
		return nil, asterism.ErrFailed
	}

	raw, err := asterism.Fetch(mirrorBase+location, "", nil, true)
	if err != nil {
		return nil, err
	}
	reader, err := zstd.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	records, err := parsePrimary(reader)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, asterism.ErrFailed
	}
	return records, nil
}

// 流式解析
func parsePrimary(reader io.Reader) ([]*asterism.Package, error) {
	decoder := xml.NewDecoder(reader)
	decoder.Strict = false

	var out []*asterism.Package
	seen := map[string]bool{}
	var record *asterism.Package
	var capture *string
	var name, arch, license, group, vendor, summary, description, url string
	var version, release, packageSize, checksumKind, checksum, location string
	text := strings.Builder{}

	captureInto := func(target *string) { capture = target; text.Reset() }
	store := func() {
		if capture == nil {
			return
		}
		*capture = strings.TrimSpace(text.String())
		capture = nil
	}
	flush := func() {
		store()
		if record != nil && (arch == "x86_64" || arch == "noarch") && name != "" &&
			!seen[name] && !excludedPackage(name, []string{
			"lib", "python", "perl", "ruby", "php", "node", "npm", "ghc", "go1",
			"texlive", "lua", "gcc", "llvm", "clang", "golang", "rust", "crates",
			"kernel", "firmware", "typelib", "erlang", "elixir", "ocaml", "r-",
			"font-", "xorg-", "qt5-", "qt6-", "gstreamer1", "cmake", "vulkan-",
		}, []string{
			"-devel", "-doc", "-docs", "-lang", "-data", "-debuginfo",
			"-debugsource", "-32bit", "-devel-static", "-nls",
		}, true) {
			seen[name] = true
			record.Origin = name
			record.Name = name
			record.Summary = summary
			record.Description = strings.TrimSpace(summary + "\n" + description)
			record.Repository = asterism.NormalizeRepository(url)
			record.Version = strings.TrimSpace(version + "-" + release)
			if size, err := strconv.ParseInt(packageSize, 10, 64); err == nil {
				record.Size, record.Unit = size, "package"
			}
			if license != "" {
				record.License = asterism.NormalizeLicense(license)
			}
			if group != "" {
				record.Label = map[string][]string{"category": strings.Split(group, "/")}
			}
			if vendor != "" && vendor != "openSUSE" {
				record.Label["vendor"] = []string{vendor}
			}
			links := map[string]string{}
			if url != "" {
				links["homepage"] = url
			}
			if location != "" {
				links["download"] = mirrorBase + location
			}
			if len(links) > 0 {
				record.Link = links
			}
			if checksum != "" {
				record.Hash = map[string]string{checksumKind: checksum}
			}
			out = append(out, record)
		}
		record = nil
		name, arch, license, group, vendor, summary, description, url = "", "", "", "", "", "", "", ""
		version, release, packageSize, checksumKind, checksum, location = "", "", "", "", "", ""
	}

	for {
		token, err := decoder.Token()
		if err != nil {
			if len(out) == 0 {
				return nil, err
			}
			break
		}
		switch element := token.(type) {
		case xml.StartElement:
			if record == nil {
				if element.Name.Local == "package" && element.Name.Space != "" {
					record = &asterism.Package{
						Source: "opensuse", Platform: []string{"Linux"},
					}
				}
				continue
			}
			switch element.Name.Local {
			case "name":
				captureInto(&name)
			case "arch":
				captureInto(&arch)
			case "summary":
				captureInto(&summary)
			case "description":
				captureInto(&description)
			case "url":
				captureInto(&url)
			case "license":
				captureInto(&license)
			case "group":
				captureInto(&group)
			case "vendor":
				captureInto(&vendor)
			case "version":
				for _, attribute := range element.Attr {
					switch attribute.Name.Local {
					case "ver":
						version = attribute.Value
					case "rel":
						release = attribute.Value
					}
				}
			case "size":
				for _, attribute := range element.Attr {
					if attribute.Name.Local == "package" {
						packageSize = attribute.Value
					}
				}
			case "checksum":
				for _, attribute := range element.Attr {
					if attribute.Name.Local == "type" {
						checksumKind = attribute.Value
					}
				}
				captureInto(&checksum)
			case "location":
				for _, attribute := range element.Attr {
					if attribute.Name.Local == "href" {
						location = attribute.Value
					}
				}
			}
		case xml.CharData:
			if capture != nil && record != nil {
				text.Write(element)
			}
		case xml.EndElement:
			if record == nil {
				continue
			}
			if capture != nil && element.Name.Local != "package" {
				store()
			}
			if element.Name.Local == "package" {
				flush()
			}
		}
	}
	return out, nil
}
