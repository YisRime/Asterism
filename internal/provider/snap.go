package provider

import (
	"encoding/json"
	"net/url"
	"runtime"
	"slices"
	"strconv"
	"strings"

	asterism "asterism/internal"
)

// 软件结构
type snapPackage struct {
	Name           string   `json:"name"`
	PackageName    string   `json:"package_name"`
	Title          string   `json:"title"`
	Summary        string   `json:"summary"`
	Version        string   `json:"version"`
	License        string   `json:"license"`
	Publisher      string   `json:"publisher"`
	DeveloperName  string   `json:"developer_name"`
	Website        string   `json:"website"`
	Contact        string   `json:"contact"`
	SupportURL     string   `json:"support_url"`
	Confinement    string   `json:"confinement"`
	DownloadURL    string   `json:"download_url"`
	DownloadSize   int64    `json:"download_size"`
	IconURL        string   `json:"icon_url"`
	ScreenshotURLs []string `json:"screenshot_urls"`
	RatingsAverage float64  `json:"ratings_average"`
	Keywords       []string `json:"keywords"`
	LastUpdated    string   `json:"last_updated"`
	DatePublished  string   `json:"date_published"`
}

// 页面结构
type snapPage struct {
	Embedded struct {
		Packages []snapPackage `json:"clickindex:package"`
	} `json:"_embedded"`
	Links struct {
		Last struct {
			Href string `json:"href"`
		} `json:"last"`
	} `json:"_links"`
	Total int `json:"total"`
}

// 解析分页
func parsePage(content []byte) ([]*asterism.Package, error) {
	var response snapPage
	if err := json.Unmarshal(content, &response); err != nil {
		return nil, err
	}
	records := make([]*asterism.Package, 0, len(response.Embedded.Packages))
	for _, snap := range response.Embedded.Packages {
		identifier := strings.TrimSpace(snap.PackageName)
		if identifier == "" {
			identifier = strings.TrimSpace(snap.Name)
			if index := strings.IndexByte(identifier, '.'); index > 0 {
				identifier = identifier[:index]
			}
		}
		if identifier == "" {
			continue
		}
		name := strings.TrimSpace(snap.Title)
		if name == "" {
			name = identifier
		}
		homepage := strings.TrimSpace(snap.Website)
		published := asterism.NormalizeTime(snap.LastUpdated)
		if published == "" {
			published = asterism.NormalizeTime(snap.DatePublished)
		}
		var maintainers []string
		for _, person := range []string{snap.DeveloperName, snap.Publisher} {
			if person = strings.TrimSpace(person); person != "" {
				maintainers = append(maintainers, person)
			}
		}
		links := map[string]string{}
		if homepage != "" {
			links["homepage"] = homepage
		}
		for _, item := range []struct{ key, value string }{
			{"download", snap.DownloadURL},
			{"icon", snap.IconURL},
			{"contact", snap.Contact},
			{"support", snap.SupportURL},
		} {
			if item.value != "" {
				links[item.key] = item.value
			}
		}
		if len(snap.ScreenshotURLs) > 0 {
			links["screenshot"] = snap.ScreenshotURLs[0]
		}
		record := &asterism.Package{
			Source: "snap", Origin: identifier, Name: name,
			Summary: snap.Summary, Repository: asterism.NormalizeRepository(homepage),
			License: asterism.NormalizeLicense(snap.License),
			Author:  snap.Publisher, Maintainer: maintainers, Version: snap.Version,
			Change: published, Popularity: snap.RatingsAverage,
			Size: snap.DownloadSize, Unit: "download",
			Label: map[string][]string{
				"keyword":     snap.Keywords,
				"confinement": {snap.Confinement},
			},
			Platform: []string{"Linux"}, Link: links,
		}
		records = append(records, record)
	}
	return records, nil
}

// 合并记录
func addPackages(index map[string]*asterism.Package, records []*asterism.Package, section string) {
	for _, record := range records {
		if previous, found := index[record.Origin]; found {
			previous.Label["repo"] = append(previous.Label["repo"], section)
			continue
		}
		if record.Label == nil {
			record.Label = map[string][]string{}
		}
		record.Label["repo"] = append(record.Label["repo"], section)
		index[record.Origin] = record
	}
}

// 去重排序
func dedupe(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	unique := values[:0]
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		unique = append(unique, value)
	}
	slices.Sort(unique)
	return slices.Compact(unique)
}

// 枚举抓取
func ParseSnap() ([]*asterism.Package, error) {
	content, err := asterism.Fetch("https://api.snapcraft.io/api/v1/snaps/sections", "application/json",
		map[string]string{"X-Ubuntu-Series": "16"}, true)
	if err != nil {
		return nil, err
	}
	var sectionResponse struct {
		Embedded struct {
			Sections []struct {
				Name string `json:"name"`
			} `json:"clickindex:sections"`
		} `json:"_embedded"`
	}
	if err := json.Unmarshal(content, &sectionResponse); err != nil {
		return nil, err
	}
	sections := make([]string, 0, len(sectionResponse.Embedded.Sections))
	seen := make(map[string]bool, len(sectionResponse.Embedded.Sections))
	for _, section := range sectionResponse.Embedded.Sections {
		name := strings.TrimSpace(section.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		sections = append(sections, name)
	}
	if len(sections) == 0 {
		return nil, asterism.ErrFailed
	}

	index := make(map[string]*asterism.Package, 8000)

	for _, section := range sections {
		first, err := asterism.Fetch("https://api.snapcraft.io/api/v1/snaps/search?section="+
			url.QueryEscape(section)+"&page=1&size=100", "application/json",
			map[string]string{"X-Ubuntu-Series": "16"}, true)
		if err != nil {
			continue
		}
		records, err := parsePage(first)
		if err != nil {
			continue
		}
		addPackages(index, records, section)

		last := 1
		var bounds snapPage
		if err := json.Unmarshal(first, &bounds); err == nil {
			found := 0
			if address, err := url.Parse(strings.TrimSpace(bounds.Links.Last.Href)); err == nil {
				if number, err := strconv.Atoi(address.Query().Get("page")); err == nil && number >= 1 {
					found = number
				}
			}
			if found > 0 {
				last = min(found, 200)
			} else if bounds.Total > 0 {
				size := max(50, len(records))
				last = min(max((bounds.Total+size-1)/size, 1), 200)
			}
		}
		if last < 2 {
			continue
		}
		batches := fetchPages(last-1, runtime.NumCPU(), func(pageIndex int) ([]*asterism.Package, error) {
			body, err := asterism.Fetch("https://api.snapcraft.io/api/v1/snaps/search?section="+
				url.QueryEscape(section)+"&page="+strconv.Itoa(pageIndex+2)+"&size=100", "application/json",
				map[string]string{"X-Ubuntu-Series": "16"}, true)
			if err != nil {
				return nil, err
			}
			return parsePage(body)
		})
		for _, batch := range batches {
			addPackages(index, batch, section)
		}
	}

	if len(index) == 0 {
		return nil, asterism.ErrFailed
	}
	final := make([]*asterism.Package, 0, len(index))
	for _, record := range index {
		record.Label["repo"] = dedupe(record.Label["repo"])
		final = append(final, record)
	}
	return final, nil
}
