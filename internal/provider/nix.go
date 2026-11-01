package provider

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"strings"

	"github.com/andybalholm/brotli"

	asterism "asterism/internal"
)

// 许可结构
type nixLicense struct {
	SPDXID   string `json:"spdxId"`
	FullName string `json:"fullName"`
	Free     bool   `json:"free"`
}

// 元数据项
type nixMeta struct {
	Description     string          `json:"description"`
	LongDescription json.RawMessage `json:"longDescription"`
	Homepage        json.RawMessage `json:"homepage"`
	License         json.RawMessage `json:"license"`
	Platforms       json.RawMessage `json:"platforms"`
	Maintainers     json.RawMessage `json:"maintainers"`
	Changelog       json.RawMessage `json:"changelog"`
	MainProgram     string          `json:"mainProgram"`
	Position        string          `json:"position"`
	Broken          bool            `json:"broken"`
	Unfree          bool            `json:"unfree"`
	Unsupported     bool            `json:"unsupported"`
	Available       *bool           `json:"available"`
}

// 软件结构
type nixPackage struct {
	Pname   string  `json:"pname"`
	Name    string  `json:"name"`
	Version string  `json:"version"`
	Meta    nixMeta `json:"meta"`
}

// 兼容文本
func nixText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil && len(list) > 0 {
		return strings.TrimSpace(list[0])
	}
	return ""
}

// 兼容许可
func licenseKey(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var single nixLicense
	if err := json.Unmarshal(raw, &single); err == nil {
		if single.SPDXID != "" {
			return single.SPDXID
		}
		return single.FullName
	}
	var list []nixLicense
	if err := json.Unmarshal(raw, &list); err == nil {
		for _, item := range list {
			if item.SPDXID != "" {
				return item.SPDXID
			}
		}
		if len(list) > 0 {
			return list[0].FullName
		}
	}
	return ""
}

// 兼容平台
func nixPlatforms(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}
	var items []any
	if err := json.Unmarshal(raw, &items); err == nil {
		out := make([]string, 0, len(items))
		for _, item := range items {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	}
	var mapping map[string]any
	if err := json.Unmarshal(raw, &mapping); err == nil {
		out := make([]string, 0, len(mapping))
		for key := range mapping {
			out = append(out, key)
		}
		slices.Sort(out)
		return out
	}
	return nil
}

// 维护兼容
func nixMaintainers(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var out []string
	var list []any
	if err := json.Unmarshal(raw, &list); err == nil {
		for _, item := range list {
			switch current := item.(type) {
			case string:
				if current != "" {
					out = append(out, current)
				}
			case map[string]any:
				if name, ok := current["name"].(string); ok && name != "" {
					out = append(out, name)
				}
			}
		}
		return out
	}
	var single map[string]any
	if err := json.Unmarshal(raw, &single); err == nil {
		if name, ok := single["name"].(string); ok && name != "" {
			out = append(out, name)
		}
	}
	return out
}

// 转换记录
func nixEntry(attribute string, pkg nixPackage) *asterism.Package {
	if attribute == "" || pkg.Meta.Broken || pkg.Meta.Unfree || pkg.Meta.Unsupported ||
		pkg.Meta.Available != nil && !*pkg.Meta.Available || slices.ContainsFunc([]string{
		"python3Packages.", "pythonPackages.", "python3.pkgs.", "python27Packages.",
		"perlPackages.", "rubyPackages.", "nodePackages.", "nodePackages_latest.",
		"haskellPackages.", "rPackages.", "luaPackages.", "emacsPackages.",
		"vimPlugins.", "texlive.", "texlivePackages.", "ocamlPackages.",
		"coqPackages.", "agdaPackages.", "idrisPackages.", "beamPackages.",
		"beamMinimalPackages.", "phpPackages.", "dotnetPackages.", "gnatPackages.",
		"gnomeExtensions.", "kdeFrameworks.", "libsForQt5.", "libsForQt6.",
		"qt5.", "qt6.", "xorg.", "linuxPackages.", "linuxPackages_latest.",
		"linuxKernel.", "linux_", "buildLinux.", "buildPackages.", "tests.",
	}, func(prefix string) bool { return strings.HasPrefix(attribute, prefix) }) {
		return nil
	}
	name := pkg.Pname
	if name == "" {
		name = pkg.Name
	}
	if name == "" {
		name = attribute
	}
	homepage := nixText(pkg.Meta.Homepage)
	description := nixText(pkg.Meta.LongDescription)
	if description == "" {
		description = pkg.Meta.Description
	}

	hasLinux, hasMac := false, false
	for _, platform := range nixPlatforms(pkg.Meta.Platforms) {
		if strings.Contains(platform, "linux") {
			hasLinux = true
		}
		if strings.Contains(platform, "darwin") {
			hasMac = true
		}
	}
	platforms := make([]string, 0, 2)
	if hasLinux {
		platforms = append(platforms, "Linux")
	}
	if hasMac {
		platforms = append(platforms, "macOS")
	}
	if len(platforms) == 0 {
		platforms = []string{"Linux"}
	}

	links := map[string]string{}
	if homepage != "" {
		links["homepage"] = homepage
	}
	if changelog := nixText(pkg.Meta.Changelog); changelog != "" {
		links["changelog"] = changelog
	}
	label := map[string][]string{}
	if pkg.Meta.MainProgram != "" {
		label["bin"] = []string{pkg.Meta.MainProgram}
	}
	extra := map[string]any{}
	if pkg.Meta.Position != "" {
		extra["position"] = pkg.Meta.Position
	}
	record := &asterism.Package{
		Source: "nix", Origin: attribute, Name: name,
		Summary: pkg.Meta.Description, Description: description,
		Repository: asterism.NormalizeRepository(homepage),
		License:    asterism.NormalizeLicense(licenseKey(pkg.Meta.License)),
		Maintainer: nixMaintainers(pkg.Meta.Maintainers),
		Version:    pkg.Version, Platform: platforms,
		Label: label, Link: links, Extra: extra,
	}
	return record
}

// 流式解析
func parseNix(reader io.Reader) ([]*asterism.Package, error) {
	decoder := json.NewDecoder(reader)
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil, asterism.ErrFailed
	}

	var out []*asterism.Package
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if key != "packages" {
			var skip json.RawMessage
			if err := decoder.Decode(&skip); err != nil {
				return nil, err
			}
			continue
		}

		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if delim, ok := token.(json.Delim); !ok || delim != '{' {
			return nil, asterism.ErrFailed
		}
		for decoder.More() {
			attribute, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			var pkg nixPackage
			if err := decoder.Decode(&pkg); err != nil {
				return nil, err
			}
			name, _ := attribute.(string)
			if record := nixEntry(name, pkg); record != nil {
				out = append(out, record)
			}
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	return out, nil
}

// 解析索引
func ParseNix() ([]*asterism.Package, error) {
	raw, err := asterism.Fetch("https://channels.nixos.org/nixpkgs-unstable/packages.json.br", "", nil, true)
	if err != nil {
		return nil, err
	}
	records, err := parseNix(brotli.NewReader(bytes.NewReader(raw)))
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, asterism.ErrFailed
	}
	return records, nil
}
