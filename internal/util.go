package asterism

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// 抓取数据
func Fetch(address, accept string, headers map[string]string, useCache bool) ([]byte, error) {
	cacheFile := cachePath(address)
	if useCache {
		if content, err := os.ReadFile(cacheFile); err == nil {
			return content, nil
		}
	}
	var lastError error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(5 * time.Second)
		}
		body, status, err := Request(address, accept, "", headers, 300*time.Second)
		if err != nil {
			lastError = err
			if status > 0 && status < 500 && status != http.StatusTooManyRequests {
				break
			}
			continue
		}
		saveCache(cacheFile, body)
		return body, nil
	}
	return nil, lastError
}

// 抓取订阅
func FetchFeed(address, accept string, maxAge, timeout time.Duration) ([]byte, error) {
	cacheFile := cachePath(address)
	if info, err := os.Stat(cacheFile); err == nil {
		if maxAge <= 0 || time.Since(info.ModTime()) < maxAge {
			if content, err := os.ReadFile(cacheFile); err == nil && len(content) > 0 {
				return content, nil
			}
		}
	}
	body, _, err := Request(address, accept, "", nil, timeout)
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		temp := cacheFile + ".tmp"
		if err := os.WriteFile(temp, body, 0o644); err == nil {
			_ = os.Rename(temp, cacheFile)
		}
	}
	return body, nil
}

// 解压数据
func Unzip(raw []byte) []byte {
	if len(raw) < 2 || raw[0] != 0x1f || raw[1] != 0x8b {
		return raw
	}
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return raw
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil {
		return raw
	}
	return content
}

// 归档遍历
func WalkArchive(raw []byte, visit func(name string, size int64, reader io.Reader) error) error {
	compressed, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer compressed.Close()

	archive := tar.NewReader(compressed)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if err := visit(header.Name, header.Size, archive); err != nil {
			return err
		}
	}
}

// 并发处理
func Parallel[Item, Result any](items []Item, workers int, handle func(Item) Result) []Result {
	results := make([]Result, len(items))
	if len(items) == 0 {
		return results
	}
	if workers < 1 {
		workers = 1
	}
	workers = min(workers, len(items))

	jobs := make(chan int)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			for index := range jobs {
				results[index] = handle(items[index])
			}
		}()
	}
	for index := range items {
		jobs <- index
	}
	close(jobs)
	group.Wait()
	return results
}

// 写入缓存
func saveCache(path string, body []byte) {
	temp := path + ".tmp"
	if err := os.WriteFile(temp, body, 0o644); err == nil {
		_ = os.Rename(temp, path)
	}
}

// 缓存路径
func cachePath(address string) string {
	sum := sha256.Sum256([]byte(address))
	return filepath.Join("data/cache", hex.EncodeToString(sum[:])[:24])
}

// 共享连接
var sharedTransport = &http.Transport{
	DialContext:         (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
	MaxIdleConns:        64,
	MaxIdleConnsPerHost: 32,
	IdleConnTimeout:     90 * time.Second,
	TLSHandshakeTimeout: 10 * time.Second,
}

// 单次请求
func Request(address, accept, token string, headers map[string]string, timeout time.Duration) ([]byte, int, error) {
	request, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36")
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := (&http.Client{Timeout: timeout, Transport: sharedTransport}).Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return nil, response.StatusCode, fmt.Errorf("HTTP %d: %s", response.StatusCode, address)
	}
	body, err := io.ReadAll(response.Body)
	return body, 0, err
}

// 解析数值
func Number(value any) int64 {
	switch current := value.(type) {
	case float64:
		return int64(current)
	case int64:
		return current
	case int:
		return int64(current)
	case json.Number:
		if count, err := current.Int64(); err == nil {
			return count
		}
		if decimal, err := current.Float64(); err == nil {
			return int64(decimal)
		}
	case string:
		text := strings.ReplaceAll(strings.TrimSpace(current), ",", "")
		if text == "" {
			return 0
		}
		if decimal, err := strconv.ParseFloat(text, 64); err == nil {
			return int64(decimal)
		}
	}
	return 0
}

// 秒级时间
func UnixTime(seconds int64) string {
	if seconds <= 0 {
		return ""
	}
	return time.Unix(seconds, 0).UTC().Format("2006-01-02T15:04:05Z")
}

// 解析时间
func ParseTime(raw string) (time.Time, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		time.RFC3339Nano, time.RFC3339,
		"2006-01-02", "2006-01-02 15:04:05", "2006-01-02T15:04:05",
	} {
		if moment, err := time.Parse(layout, text); err == nil {
			return moment.UTC(), true
		}
	}
	return time.Time{}, false
}

// 归一时间
func NormalizeTime(raw string) string {
	if moment, ok := ParseTime(raw); ok {
		return moment.UTC().Format("2006-01-02T15:04:05Z")
	}
	return ""
}

// 归一许可
func NormalizeLicense(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	if lower == "none" || lower == "null" || lower == "n/a" || lower == "-" {
		return ""
	}

	if !strings.Contains(text, " ") && !strings.Contains(text, "://") {
		if identifier := canonical(text); identifier != "" {
			return identifier
		}
	}

	if map[string]bool{
		"proprietary": true, "freeware": true, "shareware": true, "trial": true,
		"trialware": true, "adware": true, "commercial": true, "closed": true,
		"closed source": true, "closed-source": true, "unknown": true,
		"no license file provided for this software": true, "custom": true,
		"all rights reserved": true, "nolicense": true, "non-commercial": true,
		"personal use only": true,
	}[lower] {
		return "proprietary"
	}
	if reference, ok := strings.CutPrefix(lower, "licenseref-"); ok {
		if strings.Contains(reference, "proprietary") {
			return "proprietary"
		}
		return ""
	}
	if slices.ContainsFunc([]string{
		"eula", "terms of use", "terms-of-use", "terms of service",
		"terms-of-service", "commercial license",
	}, func(marker string) bool { return strings.Contains(lower, marker) }) {
		return "proprietary"
	}

	isLink := strings.Contains(text, "://")
	isPath := strings.Contains(text, "/") && strings.Contains(text, ".") && !strings.Contains(text, " ")
	if isLink || isPath {
		for _, rule := range []struct {
			pattern *regexp.Regexp
			build   func(match []string) string
		}{
			{regexp.MustCompile(`(?i)gnu\.org/licenses/(?:old-)?(?:licenses/)?gpl-(2\.0|3\.0)(\.html|\.txt)?$`),
				func(match []string) string { return "GPL-" + match[1] + "-or-later" }},
			{regexp.MustCompile(`(?i)gnu\.org/licenses/(?:old-)?(?:licenses/)?lgpl-(2\.1|3\.0)(\.html|\.txt)?$`),
				func(match []string) string { return "LGPL-" + match[1] + "-or-later" }},
			{regexp.MustCompile(`(?i)gnu\.org/licenses/(?:old-)?(?:licenses/)?agpl-(3\.0)(\.html|\.txt)?$`),
				func(match []string) string { return "AGPL-" + match[1] + "-or-later" }},
			{regexp.MustCompile(`(?i)gnu\.org/licenses/(?:old-)?(?:licenses/)?(fdl)(\.html|\.txt)?$`),
				func(match []string) string { return "GFDL-1.3-or-later" }},
			{regexp.MustCompile(`(?i)gnu\.org/copyleft/(lesser/)?gpl\.html`),
				func(match []string) string {
					if match[1] == "" {
						return "GPL-3.0-or-later"
					}
					return "LGPL-3.0-or-later"
				}},
			{regexp.MustCompile(`(?i)opensource\.org/licenses/(MIT|ISC|BSD-2-Clause|BSD-3-Clause|Zlib|MPL-2\.0|Apache-2\.0|EPL-1\.0|Unlicense)(\.html|\.txt|\.php)?$`),
				func(match []string) string { return match[1] }},
			{regexp.MustCompile(`(?i)(?:www\.)?apache\.org/licenses/LICENSE-(2\.0)(\.html|\.txt)?$`),
				func(match []string) string { return "Apache-2.0" }},
			{regexp.MustCompile(`(?i)mozilla\.org/MPL/(2\.0|2\.1)(/|\.html|/index\.html)?$`),
				func(match []string) string { return "MPL-" + match[1] }},
			{regexp.MustCompile(`(?i)(?:www\.)?creativecommons\.org/licenses/(by|by-sa)/(4\.0|3\.0|2\.5|2\.0)`),
				func(match []string) string { return "CC-" + strings.ToUpper(match[1]) + "-" + match[2] }},
			{regexp.MustCompile(`(?i)creativecommons\.org/publicdomain/zero/1\.0`),
				func(match []string) string { return "CC0-1.0" }},
		} {
			if match := rule.pattern.FindStringSubmatch(text); match != nil {
				return rule.build(match)
			}
		}
		return ""
	}

	for _, rule := range []struct {
		pattern *regexp.Regexp
		value   string
	}{
		{regexp.MustCompile(`(?i)\bapache\s*(?:license)?[, ]*(?:version)?\s*2(?:\.0)?\b`), "Apache-2.0"},
		{regexp.MustCompile(`(?i)\bmit\s+license\b`), "MIT"},
		{regexp.MustCompile(`(?i)\bbsd[\s-]*3[\s-]*clause\b`), "BSD-3-Clause"},
		{regexp.MustCompile(`(?i)\bbsd[\s-]*2[\s-]*clause\b`), "BSD-2-Clause"},
		{regexp.MustCompile(`(?i)\bmozilla public license\s*(?:version)?\s*2(?:\.0)?\b`), "MPL-2.0"},
		{regexp.MustCompile(`(?i)\bagpl\b`), "AGPL-3.0-or-later"},
		{regexp.MustCompile(`(?i)\bisc\s+license\b`), "ISC"},
		{regexp.MustCompile(`(?i)\bzlib\s+license\b`), "Zlib"},
	} {
		if rule.pattern.MatchString(text) {
			return rule.value
		}
	}

	if value, ok := map[string]string{
		"gpl": "GPL-1.0-or-later", "gpl2": "GPL-2.0-or-later", "gpl3": "GPL-3.0-or-later",
		"gplv2": "GPL-2.0-or-later", "gplv3": "GPL-3.0-or-later",
		"gpl-2": "GPL-2.0-or-later", "gpl-3": "GPL-3.0-or-later",
		"gplv2+": "GPL-2.0-or-later", "gplv3+": "GPL-3.0-or-later",
		"gplv2.0+": "GPL-2.0-or-later", "gplv3.0+": "GPL-3.0-or-later",
		"lgpl": "LGPL-2.1-or-later", "lgpl2": "LGPL-2.1-or-later", "lgpl2.1": "LGPL-2.1-or-later",
		"lgpl3": "LGPL-3.0-or-later", "lgplv3": "LGPL-3.0-or-later",
		"lgplv2.1": "LGPL-2.1-or-later", "lgplv2.1+": "LGPL-2.1-or-later",
		"lgplv3+": "LGPL-3.0-or-later",
		"agpl":    "AGPL-3.0-or-later", "agpl3": "AGPL-3.0-or-later", "agplv3": "AGPL-3.0-or-later",
		"agplv3+": "AGPL-3.0-or-later",
		"apache":  "Apache-2.0", "apache2": "Apache-2.0", "apache2.0": "Apache-2.0",
		"apache-2": "Apache-2.0", "apache license 2.0": "Apache-2.0",
		"apache license, version 2.0": "Apache-2.0", "apache software license": "Apache-2.0",
		"bsd": "BSD-2-Clause", "bsd2": "BSD-2-Clause", "bsd3": "BSD-3-Clause",
		"bsd-2": "BSD-2-Clause", "bsd-3": "BSD-3-Clause",
		"bsd2-clause": "BSD-2-Clause", "bsd3-clause": "BSD-3-Clause",
		"bsd 2-clause": "BSD-2-Clause", "bsd 3-clause": "BSD-3-Clause",
		"new bsd": "BSD-3-Clause", "simplified bsd": "BSD-2-Clause",
		"isc": "ISC", "mit license": "MIT", "the mit license": "MIT", "mit/x11": "MIT",
		"mpl": "MPL-2.0", "mpl2": "MPL-2.0",
		"mozilla public license 2.0":          "MPL-2.0",
		"mozilla public license, version 2.0": "MPL-2.0",
		"zlib":                                "Zlib", "wtfpl": "WTFPL", "unlicense": "Unlicense",
		"public domain": "Unlicense", "cc0": "CC0-1.0",
		"epl": "EPL-1.0", "epl2": "EPL-2.0", "artistic": "Artistic-2.0",
		"postgresql": "PostgreSQL", "python": "Python-2.0", "psf": "Python-2.0",
		"ruby": "Ruby", "php": "PHP-3.01", "boost": "BSL-1.0",
		"boost software license": "BSL-1.0", "openssl": "OpenSSL",
	}[lower]; ok {
		return value
	}

	if match := regexp.MustCompile(`^(agpl|lgpl|gpl)[-_ ]?v?(\d(?:\.\d)?)(\+| or later)?$`).FindStringSubmatch(lower); match != nil {
		family := strings.ToUpper(match[1])
		versionNumber := version(match[2])
		if match[3] != "" || family == "GPL" {
			return family + "-" + versionNumber + "-or-later"
		}
		return family + "-" + versionNumber + "-only"
	}
	return ""
}

// 归一仓库
func NormalizeRepository(raw string) string {
	address := strings.TrimSpace(raw)
	if address == "" {
		return ""
	}
	if !strings.Contains(address, "://") {
		address = "https://" + address
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Host), "www.")
	if !slices.Contains([]string{
		"github.com", "gitlab.com", "codeberg.org", "git.sr.ht", "bitbucket.org",
		"gitea.com", "notabug.org", "gitlab.gnome.org", "invent.kde.org",
		"gitlab.freedesktop.org",
	}, host) {
		return ""
	}
	path := strings.Trim(parsed.Path, "/")
	best := -1
	for _, marker := range []string{
		"/tree/", "/blob/", "/raw/", "/releases", "/issues", "/wiki",
		"/commit/", "/commits", "/actions", "/packages", "/pulls",
		"/tags", "/branches", "/-/", "/archive/", "/zipball", "/tarball",
		"/blame/", "/edit/",
	} {
		if index := strings.Index(path, marker); index > 0 && (best < 0 || index < best) {
			best = index
		}
	}
	if best > 0 {
		path = path[:best]
	}
	path = strings.TrimSuffix(path, ".git")
	if path == "" || !strings.Contains(path, "/") {
		return ""
	}
	canonicalPath := host + "/" + strings.ToLower(path)
	if host == "github.com" {
		parts := strings.Split(canonicalPath, "/")
		if len(parts) == 3 {
			owner, repo := parts[1], parts[2]
			if slices.Contains([]string{"chocolatey-community", "flathub", "scoopinstaller", "snapcrafters"}, owner) ||
				slices.Contains([]string{"chocolatey-packages", "choco-packages"}, repo) ||
				strings.HasSuffix(repo, "-snap") || strings.HasSuffix(repo, "-appimage") {
				return ""
			}
		}
	}
	return canonicalPath
}

// 归一名称
func NormalizeName(name string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(name) {
		if character == '_' {
			continue
		}
		if unicode.IsLetter(character) || unicode.IsNumber(character) {
			builder.WriteRune(character)
		}
	}
	cleaned := builder.String()
	for _, suffix := range []string{
		"forwindows", "forlinux", "formacos", "forandroid",
		"mediaplayer", "player", "app", "desktop", "portable",
	} {
		if len(cleaned) > len(suffix)+2 && strings.HasSuffix(cleaned, suffix) {
			cleaned = cleaned[:len(cleaned)-len(suffix)]
		}
	}
	return cleaned
}

// 通用节点
type Node struct {
	name       xml.Name
	attributes map[string]string
	text       string
	nodes      []*Node
}

// 递归解析
func (node *Node) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	node.name = start.Name
	if len(start.Attr) > 0 {
		node.attributes = make(map[string]string, len(start.Attr))
		for _, attribute := range start.Attr {
			node.attributes[attribute.Name.Local] = attribute.Value
		}
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch element := token.(type) {
		case xml.StartElement:
			if element.Name.Local == "content" {
				if err := decoder.Skip(); err != nil {
					return err
				}
				continue
			}
			child := &Node{}
			if err := child.UnmarshalXML(decoder, element); err != nil {
				return err
			}
			node.nodes = append(node.nodes, child)
		case xml.CharData:
			node.text += string(element)
		case xml.EndElement:
			return nil
		}
	}
}

// 取子节点
func (node *Node) Child(local string) *Node {
	for _, child := range node.nodes {
		if child.name.Local == local {
			return child
		}
	}
	return nil
}

// 子节点集
func (node *Node) Children(local string) []*Node {
	var nodes []*Node
	for _, child := range node.nodes {
		if child.name.Local == local {
			nodes = append(nodes, child)
		}
	}
	return nodes
}

// 取子文本
func (node *Node) TextOf(local string) string {
	if child := node.Child(local); child != nil {
		return strings.TrimSpace(child.text)
	}
	return ""
}

// 节点文本
func (node *Node) Text() string { return strings.TrimSpace(node.text) }

// 取属性值
func (node *Node) Attribute(key string) string {
	if node.attributes == nil {
		return ""
	}
	return node.attributes[key]
}

// 解码文档
func Decode(raw []byte) (*Node, error) {
	root := &Node{}
	if err := xml.Unmarshal(raw, root); err != nil {
		return nil, err
	}
	return root, nil
}

// 加载数据
func LoadData(address string, target any) error {
	raw, err := Fetch(address, "", nil, true)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

// 补全版本
func version(number string) string {
	if strings.Contains(number, ".") {
		return number
	}
	return number + ".0"
}

// 规范标识
func canonical(token string) string {
	text := strings.TrimSpace(token)
	if match := regexp.MustCompile(`(?i)^(AGPL|LGPL|GPL)[-_ ]?v?(\d(?:\.\d)?)(?:[-_ ]?(\+|only|later))?$`).FindStringSubmatch(text); match != nil {
		family := strings.ToUpper(match[1])
		versionNumber := version(match[2])
		switch strings.ToLower(match[3]) {
		case "", "+", "later":
			return family + "-" + versionNumber + "-or-later"
		default:
			return family + "-" + versionNumber + "-only"
		}
	}
	if regexp.MustCompile(`(?i)^(AGPL|LGPL|GPL)[-_ ]?\d(?:\.\d)?[\s+](or later)$`).MatchString(text) {
		return strings.NewReplacer(" ", "-", "_", "-").Replace(strings.ToUpper(text))
	}
	if regexp.MustCompile(`(?i)^(AGPL|LGPL|GPL)[-_]?\d(?:\.\d)?-only$`).MatchString(text) {
		return strings.ReplaceAll(strings.ToUpper(text), "_", "-")
	}

	if mapped, ok := map[string]string{
		"GPL-3.0": "GPL-3.0-or-later", "GPL3": "GPL-3.0-or-later",
		"GPL-2.0": "GPL-2.0-or-later", "GPL2": "GPL-2.0-or-later",
		"GPL-3.0+": "GPL-3.0-or-later", "GPL-2.0+": "GPL-2.0-or-later",
		"GPL-3.0-ONLY": "GPL-3.0-only", "GPL-2.0-ONLY": "GPL-2.0-only",
		"APACHE-2.0": "Apache-2.0", "APACHE 2.0": "Apache-2.0",
		"MIT LICENSE": "MIT", "THE MIT LICENSE (MIT)": "MIT",
		"BSD": "BSD-2-Clause", "BSD-2-CLAUSE": "BSD-2-Clause",
		"BSD-3-CLAUSE": "BSD-3-Clause", "NEW BSD": "BSD-3-Clause",
		"SIMPLIFIED BSD": "BSD-2-Clause", "ISC LICENSE": "ISC",
		"MPL-2.0 LICENSE": "MPL-2.0", "ZLIB/LICENSE": "Zlib",
		"PUBLIC DOMAIN": "Unlicense", "UNLICENSE/LICENSE": "Unlicense",
	}[strings.ToUpper(text)]; ok {
		return mapped
	}

	squash := func(value string) string {
		var builder strings.Builder
		for _, character := range value {
			if character == ' ' || character == '_' || character == '.' || character == '-' {
				continue
			}
			builder.WriteRune(unicode.ToUpper(character))
		}
		return builder.String()
	}
	identifiers := []string{
		"MIT", "MIT-0", "Apache-2.0", "Apache-1.1", "BSD-2-Clause", "BSD-3-Clause",
		"BSD-4-Clause", "ISC", "MPL-2.0", "MPL-1.1", "LGPL-2.1-only", "LGPL-2.1-or-later",
		"LGPL-3.0-only", "LGPL-3.0-or-later", "AGPL-3.0-only", "AGPL-3.0-or-later",
		"Zlib", "WTFPL", "Unlicense", "CC0-1.0", "EPL-1.0", "EPL-2.0", "Artistic-2.0",
		"PostgreSQL", "Python-2.0", "Ruby", "PHP-3.01", "BSL-1.0", "OpenSSL",
		"CC-BY-4.0", "CC-BY-SA-4.0", "CC-BY-3.0", "CC-BY-SA-3.0", "GFDL-1.3-or-later",
		"GPL-1.0-only", "GPL-1.0-or-later", "GPL-2.0-only", "GPL-2.0-or-later",
		"GPL-3.0-only", "GPL-3.0-or-later",
	}
	for _, identifier := range identifiers {
		if text == identifier {
			return text
		}
	}
	squashed := squash(text)
	for _, identifier := range identifiers {
		if squash(identifier) == squashed {
			return identifier
		}
	}
	return ""
}

// 数值取整
func Round(value float64, places int) float64 {
	scale := math.Pow(10, float64(places))
	return math.Round(value*scale) / scale
}
