package provider

import (
	"strconv"
	"strings"
	"time"

	asterism "asterism/internal"
)

// 解析应用
func ParseFlathub() ([]*asterism.Package, error) {
	raw, err := asterism.Fetch("https://dl.flathub.org/repo/appstream/x86_64/appstream.xml.gz", "", nil, true)
	if err != nil {
		return nil, err
	}
	root, err := asterism.Decode(asterism.Unzip(raw))
	if err != nil {
		return nil, err
	}

	var out []*asterism.Package
	seen := map[string]bool{}
	for _, component := range root.Children("component") {
		if kind := component.TextOf("type"); kind != "" && kind != "desktop" {
			continue
		}
		id := component.TextOf("id")
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true

		name := component.TextOf("name")
		if name == "" {
			parts := strings.Split(id, ".")
			name = parts[len(parts)-1]
		}

		var homepage, source string
		links := map[string]string{}
		for index, link := range component.Children("url") {
			text := strings.TrimSpace(link.Text())
			if text == "" {
				continue
			}
			kind := link.Attribute("type")
			if index == 0 {
				homepage = text
			}
			if kind == "vcs-browser" && source == "" {
				source = text
			}
			if kind != "" && kind != "homepage" && kind != "vcs-browser" {
				links[kind] = text
			}
		}
		repository := asterism.NormalizeRepository(source)
		if repository == "" {
			repository = asterism.NormalizeRepository(homepage)
		}

		description := ""
		if node := component.Child("description"); node != nil {
			parts := []string{}
			for _, paragraph := range node.Children("p") {
				if text := paragraph.Text(); text != "" {
					parts = append(parts, text)
				}
			}
			description = strings.Join(parts, "\n")
			if description == "" {
				description = node.Text()
			}
		}

		var keywords []string
		if node := component.Child("keywords"); node != nil {
			for _, item := range node.Children("keyword") {
				if text := item.Text(); text != "" {
					keywords = append(keywords, text)
				}
			}
		}

		var maintainers []string
		if name := component.TextOf("developer_name"); name != "" {
			maintainers = append(maintainers, name)
		}

		if node := component.Child("icon"); node != nil {
			if node.Attribute("type") == "remote" {
				links["icon"] = node.Text()
			}
		}
		if node := component.Child("screenshots"); node != nil {
			for _, shot := range node.Children("screenshot") {
				for _, image := range shot.Children("image") {
					if text := image.Text(); text != "" {
						links["screenshot"] = text
					}
				}
			}
		}

		var version, published string
		if releases := component.Child("releases"); releases != nil {
			var newest *asterism.Node
			var latestStamp int64 = -1
			for _, release := range releases.Children("release") {
				stamp, _ := strconv.ParseInt(release.Attribute("timestamp"), 10, 64)
				if stamp > latestStamp {
					latestStamp, newest = stamp, release
				}
			}
			if newest != nil {
				version = newest.Attribute("version")
				if latestStamp > 0 {
					published = asterism.UnixTime(latestStamp)
				} else if date := newest.Attribute("date"); date != "" {
					if moment, err := time.Parse("2006-01-02", date); err == nil {
						published = moment.UTC().Format("2006-01-02T15:04:05Z")
					}
				}
			}
		}

		var categories []string
		if node := component.Child("categories"); node != nil {
			for _, item := range node.Children("category") {
				if text := strings.TrimSpace(item.Text()); text != "" {
					categories = append(categories, text)
				}
			}
		}

		if homepage != "" {
			links["homepage"] = homepage
		}
		out = append(out, &asterism.Package{
			Source: "flathub", Origin: id, Name: name,
			Summary: component.TextOf("summary"), Description: description,
			Repository: repository,
			License:    asterism.NormalizeLicense(component.TextOf("project_license")),
			Maintainer: maintainers, Version: version, Release: published,
			Label: map[string][]string{
				"keyword":  keywords,
				"category": categories,
			},
			Link: links, Platform: []string{"Linux"},
		})
	}
	return out, nil
}
