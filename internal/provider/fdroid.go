package provider

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	asterism "asterism/internal"
)

// 本地结构
type fdroidLocalized struct {
	Name        string `json:"name"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Website     string `json:"webSite"`
}

// 应用结构
type fdroidApp struct {
	PackageName   string                     `json:"packageName"`
	AuthorName    string                     `json:"authorName"`
	AuthorWebsite string                     `json:"authorWebSite"`
	AuthorEmail   string                     `json:"authorEmail"`
	License       string                     `json:"license"`
	SourceCode    string                     `json:"sourceCode"`
	Categories    []string                   `json:"categories"`
	AntiFeatures  []string                   `json:"antiFeatures"`
	Features      []string                   `json:"features"`
	Icon          string                     `json:"icon"`
	Added         int64                      `json:"added"`
	Version       string                     `json:"suggestedVersionName"`
	LastUpdated   int64                      `json:"lastUpdated"`
	Localized     map[string]fdroidLocalized `json:"localized"`
}

// 版本结构
type fdroidPackage struct {
	VersionCode int64    `json:"versionCode"`
	VersionName string   `json:"versionName"`
	Size        int64    `json:"size"`
	Hash        string   `json:"hash"`
	Permissions []string `json:"permissions"`
}

// 版本集合
type fdroidPackages map[string][]fdroidPackage

// 解析索引
func (packages *fdroidPackages) UnmarshalJSON(raw []byte) error {
	var mapping map[string][]fdroidPackage
	if err := json.Unmarshal(raw, &mapping); err == nil {
		*packages = mapping
		return nil
	}
	var list []struct {
		PackageName string   `json:"packageName"`
		VersionCode int64    `json:"versionCode"`
		VersionName string   `json:"versionName"`
		Size        int64    `json:"size"`
		Hash        string   `json:"hash"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return err
	}
	out := fdroidPackages{}
	for _, entry := range list {
		if entry.PackageName == "" {
			continue
		}
		out[entry.PackageName] = append(out[entry.PackageName], fdroidPackage{
			VersionCode: entry.VersionCode, VersionName: entry.VersionName,
			Size: entry.Size, Hash: entry.Hash, Permissions: entry.Permissions,
		})
	}
	*packages = out
	return nil
}

// 索引结构
type fdroidIndex struct {
	Apps     []fdroidApp    `json:"apps"`
	Packages fdroidPackages `json:"packages"`
}

// 解析安卓
func ParseFdroid(source, address string) ([]*asterism.Package, error) {
	var index fdroidIndex
	if err := asterism.LoadData(address, &index); err != nil {
		return nil, err
	}

	out := make([]*asterism.Package, 0, len(index.Apps))
	for _, app := range index.Apps {
		if app.PackageName == "" {
			continue
		}
		localized := fdroidLocalized{}
		if entry, ok := app.Localized["en-US"]; ok && entry != (fdroidLocalized{}) {
			localized = entry
		} else if entry, ok := app.Localized["en"]; ok && entry != (fdroidLocalized{}) {
			localized = entry
		} else {
			keys := make([]string, 0, len(app.Localized))
			for key := range app.Localized {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			if len(keys) > 0 {
				localized = app.Localized[keys[0]]
			}
		}
		name := localized.Name
		if name == "" {
			parts := strings.Split(app.PackageName, ".")
			name = parts[len(parts)-1]
		}
		homepage := localized.Website
		if homepage == "" {
			homepage = app.AuthorWebsite
		}
		description := localized.Description
		version := app.Version
		var best fdroidPackage
		for _, candidate := range index.Packages[app.PackageName] {
			if candidate.VersionCode > best.VersionCode {
				best = candidate
			}
		}
		if version == "" {
			version = best.VersionName
		}
		var maintainers []string
		for _, person := range []string{app.AuthorName, app.AuthorEmail} {
			if person = strings.TrimSpace(person); person != "" {
				maintainers = append(maintainers, person)
			}
		}
		published := ""
		if app.LastUpdated > 0 {
			published = time.UnixMilli(app.LastUpdated).UTC().Format("2006-01-02T15:04:05Z")
		}
		links := map[string]string{}
		if homepage != "" {
			links["homepage"] = homepage
		}
		if app.Icon != "" {
			links["icon"] = strings.TrimSuffix(address, "/index-v1.json") + "/icons/" + app.Icon
		}
		record := &asterism.Package{
			Source: source, Origin: app.PackageName, Name: name,
			Summary: localized.Summary, Description: description,
			Repository: asterism.NormalizeRepository(app.SourceCode),
			License:    asterism.NormalizeLicense(app.License),
			Author:     app.AuthorName, Maintainer: maintainers, Version: version,
			Change: published, Size: best.Size, Unit: "download",
			Label: map[string][]string{
				"category":   app.Categories,
				"feature":    app.Features,
				"anti":       app.AntiFeatures,
				"permission": best.Permissions,
			},
			Platform: []string{"Android"}, Link: links,
		}
		if best.Hash != "" {
			record.Hash = map[string]string{"hash": best.Hash}
		}
		out = append(out, record)
	}
	return out, nil
}
