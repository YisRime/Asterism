package provider

import (
	"regexp"
	"runtime"
	"strconv"
	"strings"

	asterism "asterism/internal"
)

// 解析页码
func chocolatePage(skip int) ([]*asterism.Package, error) {
	address := "https://community.chocolatey.org/api/v2/Packages()?$filter=IsLatestVersion%20eq%20true&$skip=" + strconv.Itoa(skip) + "&$top=40&$orderby=Downloads%20desc"
	raw, err := asterism.Fetch(address, "application/atom+xml", nil, true)
	if err != nil {
		return nil, err
	}
	root, err := asterism.Decode(raw)
	if err != nil {
		return nil, err
	}

	var out []*asterism.Package
	pattern := regexp.MustCompile(`Id='([^']+)'`)
	for _, entry := range root.Children("entry") {
		properties := entry.Child("properties")
		if properties == nil {
			continue
		}
		packageKey := strings.TrimSpace(entry.TextOf("title"))
		if packageKey == "" {
			if matches := pattern.FindStringSubmatch(entry.TextOf("id")); matches != nil {
				packageKey = matches[1]
			}
		}
		if packageKey == "" {
			continue
		}
		title := properties.TextOf("Title")
		if title == "" {
			title = packageKey
		}
		description := properties.TextOf("Description")
		summary := properties.TextOf("Summary")
		if summary == "" && description != "" {
			if len(description) > 400 {
				summary = description[:400]
			} else {
				summary = description
			}
		}
		projectLink := properties.TextOf("ProjectUrl")
		repository := asterism.NormalizeRepository(properties.TextOf("ProjectSourceUrl"))
		if repository == "" {
			repository = asterism.NormalizeRepository(properties.TextOf("ProjectSourceCodeUrl"))
		}
		if repository == "" {
			repository = asterism.NormalizeRepository(projectLink)
		}
		published := asterism.NormalizeTime(properties.TextOf("Published"))
		if updated := asterism.NormalizeTime(properties.TextOf("LastUpdated")); updated > published {
			published = updated
		}
		var maintainers []string
		for _, group := range []string{properties.TextOf("Authors"), properties.TextOf("Owners")} {
			for _, name := range strings.Split(group, ",") {
				if name = strings.TrimSpace(name); name != "" {
					maintainers = append(maintainers, name)
				}
			}
		}
		var dependencies []string
		for _, item := range strings.Split(properties.TextOf("Dependencies"), "|") {
			if item = strings.TrimSpace(strings.SplitN(item, ":", 2)[0]); item != "" {
				dependencies = append(dependencies, item)
			}
		}
		links := map[string]string{}
		if projectLink != "" {
			links["homepage"] = projectLink
		}
		for _, item := range []struct{ key, value string }{
			{"icon", properties.TextOf("IconUrl")},
			{"license", properties.TextOf("LicenseUrl")},
			{"package", properties.TextOf("PackageSourceUrl")},
			{"notes", properties.TextOf("ReleaseNotes")},
		} {
			if item.value != "" {
				links[item.key] = item.value
			}
		}
		record := &asterism.Package{
			Source: "chocolatey", Origin: packageKey, Name: title,
			Summary: summary, Repository: repository, Description: description,
			License: asterism.NormalizeLicense(properties.TextOf("LicenseUrl")),
			Author:  properties.TextOf("PackageReviewer"), Maintainer: maintainers,
			Version: properties.TextOf("Version"), Release: published,
			Change:   asterism.NormalizeTime(properties.TextOf("LastUpdated")),
			Download: asterism.Number(properties.TextOf("Downloads")), Window: "total",
			Depend: dependencies, Label: map[string][]string{"tag": strings.Fields(properties.TextOf("Tags"))},
			Platform: []string{"Windows"}, Link: links}
		out = append(out, record)
	}
	return out, nil
}

// 解析分页
func ParseChocolatey() ([]*asterism.Package, error) {
	batches := fetchPages(300, runtime.NumCPU(), func(index int) ([]*asterism.Package, error) {
		return chocolatePage(index * 40)
	})
	var out []*asterism.Package
	for _, records := range batches {
		out = append(out, records...)
	}
	if len(out) == 0 {
		return nil, asterism.ErrFailed
	}
	return out, nil
}
