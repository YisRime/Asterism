package provider

import (
	"bytes"
	"encoding/json"
	"io"
	"path"
	"strings"

	asterism "asterism/internal"
)

// 解析清单
func parseScoop(content []byte, bucket string) ([]*asterism.Package, error) {
	var records []*asterism.Package
	err := asterism.WalkArchive(content, func(name string, size int64, reader io.Reader) error {
		if !strings.Contains(name, "/bucket/") || !strings.HasSuffix(name, ".json") {
			return nil
		}
		if size > 1<<20 {
			return nil
		}
		body, err := io.ReadAll(io.LimitReader(reader, 1<<20+1))
		if err != nil {
			return err
		}
		if len(body) > 1<<20 {
			return nil
		}
		var manifest struct {
			Version     string          `json:"version"`
			Description string          `json:"description"`
			Homepage    string          `json:"homepage"`
			License     json.RawMessage `json:"license"`
			URL         json.RawMessage `json:"url"`
			Hash        json.RawMessage `json:"hash"`
			Notes       json.RawMessage `json:"notes"`
			Depends     json.RawMessage `json:"depends"`
			Bin         json.RawMessage `json:"bin"`
		}
		if err := json.Unmarshal(body, &manifest); err != nil {
			return nil
		}
		identifier := strings.TrimSuffix(path.Base(name), ".json")
		if identifier == "" {
			return nil
		}
		license := ""
		buffer := bytes.TrimSpace(manifest.License)
		if len(buffer) > 0 && !bytes.Equal(buffer, []byte("null")) {
			var text string
			if err := json.Unmarshal(buffer, &text); err == nil {
				license = asterism.NormalizeLicense(text)
			} else {
				var object struct {
					Identifier string `json:"identifier"`
					URL        string `json:"url"`
				}
				if err := json.Unmarshal(buffer, &object); err == nil {
					if object.Identifier != "" {
						license = asterism.NormalizeLicense(object.Identifier)
					} else {
						license = asterism.NormalizeLicense(object.URL)
					}
				}
			}
		}
		link := map[string]string{}
		if manifest.Homepage != "" {
			link["homepage"] = manifest.Homepage
		}
		if download := jsonStrings(manifest.URL); len(download) > 0 {
			link["download"] = download[0]
		}
		label := map[string][]string{}
		if bins := jsonStrings(manifest.Bin); len(bins) > 0 {
			label["bin"] = bins
		}
		record := &asterism.Package{
			Source: "scoop", Origin: identifier, Name: identifier,
			Summary:    manifest.Description,
			Repository: asterism.NormalizeRepository(manifest.Homepage),
			License:    license,
			Version:    manifest.Version,
			Platform:   []string{"Windows"},
			Label:      label,
			Link:       link,
		}
		if hashes := jsonStrings(manifest.Hash); len(hashes) > 0 {
			record.Hash = map[string]string{"hash": strings.TrimPrefix(hashes[0], "sha256:")}
		}
		if notes := jsonStrings(manifest.Notes); len(notes) > 0 {
			record.Description = strings.Join(notes, "\n")
		}
		if depends := jsonStrings(manifest.Depends); len(depends) > 0 {
			record.Depend = depends
		}
		records = append(records, record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}

// 解析分桶
func ParseScoop() ([]*asterism.Package, error) {
	buckets := []repoItem{
		{"scoop_main", "https://codeload.github.com/ScoopInstaller/Main/tar.gz/refs/heads/master"},
		{"scoop_extras", "https://codeload.github.com/ScoopInstaller/Extras/tar.gz/refs/heads/master"},
	}
	var merged []*asterism.Package
	for _, bucket := range buckets {
		content, err := asterism.Fetch(bucket.URL, "", nil, true)
		if err != nil {
			return nil, err
		}
		records, err := parseScoop(content, bucket.Name)
		if err != nil {
			return nil, err
		}
		merged = append(merged, records...)
	}
	merged = uniquePackages(merged)
	return merged, nil
}
