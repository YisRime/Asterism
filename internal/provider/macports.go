package provider

import (
	"encoding/json"
	"runtime"
	"strconv"

	asterism "asterism/internal"
)

// 端口结构
type portPackage struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	License     string          `json:"license"`
	Homepage    string          `json:"homepage"`
	Description string          `json:"description"`
	Categories  []string        `json:"categories"`
	Variants    []string        `json:"variants"`
	Active      bool            `json:"active"`
	ReplacedBy  json.RawMessage `json:"replaced_by"`
	Maintainers []struct {
		Name   string `json:"name"`
		GitHub string `json:"github"`
	} `json:"maintainers"`
}

// 转换记录
func portEntry(pkg portPackage) *asterism.Package {
	if pkg.Name == "" || !pkg.Active {
		return nil
	}
	if len(pkg.ReplacedBy) > 0 && string(pkg.ReplacedBy) != "null" {
		return nil
	}
	var maintainers []string
	for _, keeper := range pkg.Maintainers {
		name := keeper.Name
		if name == "" {
			name = keeper.GitHub
		}
		if name != "" {
			maintainers = append(maintainers, name)
		}
	}
	author := ""
	if len(maintainers) > 0 {
		author = maintainers[0]
	}
	link := map[string]string{}
	if pkg.Homepage != "" {
		link["homepage"] = pkg.Homepage
	}
	return &asterism.Package{
		Source: "macports", Origin: pkg.Name, Name: pkg.Name,
		Summary: pkg.Description, Description: pkg.Description,
		Repository: asterism.NormalizeRepository(pkg.Homepage),
		License:    asterism.NormalizeLicense(pkg.License),
		Author:     author, Maintainer: maintainers, Version: pkg.Version,
		Label: map[string][]string{
			"variant":  pkg.Variants,
			"category": pkg.Categories,
		},
		Platform: []string{"macOS"}, Link: link,
	}
}

// 解析分页
func parsePorts(raw []byte) ([]*asterism.Package, int, error) {
	var response struct {
		Count   int           `json:"count"`
		Results []portPackage `json:"results"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, 0, err
	}
	records := make([]*asterism.Package, 0, len(response.Results))
	for _, pkg := range response.Results {
		if record := portEntry(pkg); record != nil {
			records = append(records, record)
		}
	}
	return records, response.Count, nil
}

// 解析全量
func ParseMacPorts() ([]*asterism.Package, error) {
	first, err := asterism.Fetch("https://ports.macports.org/api/v1/ports/?page=1", "application/json", nil, true)
	if err != nil {
		return nil, err
	}
	records, count, err := parsePorts(first)
	if err != nil {
		return nil, err
	}
	if count <= 0 {
		return nil, asterism.ErrFailed
	}
	pages := min((count+49)/50, 2000)

	batches := fetchPages(pages-1, runtime.NumCPU(), func(index int) ([]*asterism.Package, error) {
		address := "https://ports.macports.org/api/v1/ports/?page=" + strconv.Itoa(index+2)
		raw, err := asterism.Fetch(address, "application/json", nil, true)
		if err != nil {
			return nil, err
		}
		pageRecords, _, err := parsePorts(raw)
		return pageRecords, err
	})
	for _, batch := range batches {
		records = append(records, batch...)
	}
	records = uniquePackages(records)
	if len(records) == 0 {
		return nil, asterism.ErrFailed
	}
	return records, nil
}
