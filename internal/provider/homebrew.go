package provider

import (
	"encoding/json"
	"strconv"
	"strings"

	asterism "asterism/internal"
)

// 提取依赖
func jsonStrings(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var values []any
	if err := json.Unmarshal(raw, &values); err == nil {
		var out []string
		for _, value := range values {
			switch current := value.(type) {
			case string:
				if current != "" {
					out = append(out, current)
				}
			}
		}
		return out
	}
	var mapping map[string]any
	if err := json.Unmarshal(raw, &mapping); err == nil {
		var out []string
		for _, value := range mapping {
			switch current := value.(type) {
			case string:
				if current != "" {
					out = append(out, current)
				}
			case []any:
				for _, item := range current {
					if text, ok := item.(string); ok && text != "" {
						out = append(out, text)
					}
				}
			}
		}
		return out
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil && single != "" {
		return []string{single}
	}
	return nil
}

// 解析应用
func ParseCask() ([]*asterism.Package, error) {
	var data []struct {
		Token       string          `json:"token"`
		FullToken   string          `json:"full_token"`
		Name        []string        `json:"name"`
		Description string          `json:"desc"`
		Homepage    string          `json:"homepage"`
		Version     string          `json:"version"`
		URL         string          `json:"url"`
		SHA256      string          `json:"sha256"`
		Caveats     string          `json:"caveats"`
		BundleID    string          `json:"bundle_id"`
		DependsOn   json.RawMessage `json:"depends_on"`
		Deprecated  bool            `json:"deprecated"`
		Disabled    bool            `json:"disabled"`
	}
	if err := asterism.LoadData("https://formulae.brew.sh/api/cask.json", &data); err != nil {
		return nil, err
	}
	installs := ParseAnalytics("https://formulae.brew.sh/api/analytics/cask-install/homebrew-cask/30d.json", "cask")
	out := make([]*asterism.Package, 0, len(data))
	for _, cask := range data {
		if cask.Disabled || cask.Deprecated {
			continue
		}
		id := cask.Token
		if id == "" {
			id = cask.FullToken
		}
		name := id
		if len(cask.Name) > 0 && cask.Name[0] != "" {
			name = cask.Name[0]
		}
		link := map[string]string{}
		if cask.Homepage != "" {
			link["homepage"] = cask.Homepage
		}
		if cask.URL != "" {
			link["download"] = cask.URL
		}
		label := map[string][]string{}
		if cask.BundleID != "" {
			label["bundle"] = []string{cask.BundleID}
		}
		if strings.HasPrefix(id, "font-") {
			label["flag"] = []string{"font"}
		}
		record := &asterism.Package{
			Source: "brew_cask", Origin: id, Name: name,
			Summary: cask.Description, Description: strings.TrimSpace(cask.Caveats),
			Repository: asterism.NormalizeRepository(cask.Homepage),
			Version:    cask.Version, Platform: []string{"macOS"},
			Depend: jsonStrings(cask.DependsOn), Label: label, Link: link,
		}
		if cask.SHA256 != "" {
			record.Hash = map[string]string{"sha256": cask.SHA256}
		}
		if count := installs[id]; count > 0 {
			record.Download, record.Window = count, "30d"
		}
		out = append(out, record)
	}
	return out, nil
}

// 解析统计
func ParseAnalytics(address, field string) map[string]int64 {
	out := map[string]int64{}
	raw, err := asterism.Fetch(address, "", nil, true)
	if err != nil {
		return out
	}
	var data struct {
		Formulae json.RawMessage `json:"formulae"`
	}
	if err := json.Unmarshal(raw, &data); err != nil || len(data.Formulae) == 0 {
		return out
	}

	add := func(value json.RawMessage, fallbackKey string) {
		var list []map[string]any
		if err := json.Unmarshal(value, &list); err == nil && len(list) > 0 {
			for _, item := range list {
				if name, ok := item[field].(string); ok && name != "" {
					out[name] = asterism.Number(item["count"])
				}
			}
			return
		}
		var scalar any
		if err := json.Unmarshal(value, &scalar); err == nil {
			out[fallbackKey] = asterism.Number(scalar)
		}
	}

	var byName map[string]json.RawMessage
	if err := json.Unmarshal(data.Formulae, &byName); err == nil {
		for name, value := range byName {
			add(value, name)
		}
		return out
	}
	var list []map[string]any
	if err := json.Unmarshal(data.Formulae, &list); err == nil {
		for _, item := range list {
			if name, ok := item[field].(string); ok && name != "" {
				out[name] = asterism.Number(item["count"])
			}
		}
	}
	return out
}

// 兼容形状
type brewLink struct {
	URL string
}

func (item *brewLink) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		item.URL = text
		return nil
	}
	var obj struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(data, &obj); err == nil {
		item.URL = obj.URL
	}
	return nil
}

// 解析套件
func ParseFormula() ([]*asterism.Package, error) {
	var raw []json.RawMessage
	if err := asterism.LoadData("https://formulae.brew.sh/api/formula.json", &raw); err != nil {
		return nil, err
	}
	installs := ParseAnalytics("https://formulae.brew.sh/api/analytics/install/homebrew-core/30d.json", "formula")

	out := make([]*asterism.Package, 0, len(raw))
	for _, item := range raw {
		var formula struct {
			Name        string `json:"name"`
			Description string `json:"desc"`
			Homepage    string `json:"homepage"`
			License     string `json:"license"`
			Versions    struct {
				Stable string `json:"stable"`
			} `json:"versions"`
			URLs struct {
				Stable brewLink `json:"stable"`
			} `json:"urls"`
			Revision   int             `json:"revision"`
			DependsOn  json.RawMessage `json:"depends_on"`
			Deprecated bool            `json:"deprecated"`
			Disabled   bool            `json:"disabled"`
		}
		// 单条容错
		if err := json.Unmarshal(item, &formula); err != nil || formula.Name == "" {
			continue
		}
		if formula.Disabled || formula.Deprecated {
			continue
		}
		link := map[string]string{}
		if formula.Homepage != "" {
			link["homepage"] = formula.Homepage
		}
		if formula.URLs.Stable.URL != "" {
			link["download"] = formula.URLs.Stable.URL
		}
		label := map[string][]string{"flag": {"cli"}}
		if formula.Revision > 0 {
			label["flag"] = append(label["flag"], "rev"+strconv.Itoa(formula.Revision))
		}
		record := &asterism.Package{
			Source: "brew_formula", Origin: formula.Name, Name: formula.Name,
			Summary: formula.Description, Description: formula.Description,
			Repository: asterism.NormalizeRepository(formula.Homepage),
			License:    asterism.NormalizeLicense(formula.License),
			Version:    formula.Versions.Stable,
			Download:   installs[formula.Name], Window: "30d",
			Platform: []string{"macOS", "Linux"}, Depend: jsonStrings(formula.DependsOn),
			Label: label, Link: link,
		}
		out = append(out, record)
	}
	return out, nil
}
