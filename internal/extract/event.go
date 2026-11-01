package extract

import (
	"encoding/json"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"

	asterism "asterism/internal"
)

var feedParser = gofeed.NewParser()

// 补全星标
func fillStars(list []*asterism.Software, repositories map[string]*asterism.Repository, token string) int {
	now := time.Now()
	jobs := map[string]bool{}
	for _, item := range list {
		if !strings.HasPrefix(item.Repository, "github.com/") {
			continue
		}
		repository := repositories[item.Repository]
		if repository == nil {
			repository = &asterism.Repository{Name: item.Repository, Star: -1}
			repositories[item.Repository] = repository
		}
		if repository.Star != -1 {
			if parsed, err := time.Parse(time.RFC3339, repository.Stamp); err == nil &&
				now.Sub(parsed) < 7*24*time.Hour {
				continue
			}
		}
		jobs[item.Repository] = true
	}
	if len(jobs) == 0 {
		applyStars(list, repositories)
		return 0
	}
	names := make([]string, 0, len(jobs))
	for name := range jobs {
		names = append(names, name)
	}
	slices.Sort(names)
	asterism.Log("补全 %d 个星标", len(names))

	stamp := now.UTC().Format("2006-01-02T15:04:05Z")
	okCount := 0
	type outcome struct {
		name  string
		stars int
		ok    bool
	}
	for start := 0; start < len(names); start += 1000 {
		end := min(start+1000, len(names))
		batch := names[start:end]
		outcomes := asterism.Parallel(batch, runtime.NumCPU(), func(name string) outcome {
			stars, ok := fetchStars(strings.TrimPrefix(name, "github.com/"), token)
			return outcome{name: name, stars: stars, ok: ok}
		})
		for _, result := range outcomes {
			repository := repositories[result.name]
			if repository == nil {
				repository = &asterism.Repository{Name: result.name}
				repositories[result.name] = repository
			}
			if result.ok {
				repository.Star = result.stars
				okCount++
			} else {
				repository.Star = -1
			}
			repository.Stamp = stamp
			if err := asterism.SaveStar(result.name, repository.Star, repository.Stamp); err != nil {
				asterism.Log("保存星标失败：%v", err)
			}
		}
		time.Sleep(2 * time.Second)
	}
	applyStars(list, repositories)
	return okCount
}

// 回填星标
func applyStars(list []*asterism.Software, repositories map[string]*asterism.Repository) {
	for _, item := range list {
		if item.Repository == "" {
			continue
		}
		if repository, ok := repositories[item.Repository]; ok && repository.Star != -1 {
			item.Star = repository.Star
		}
	}
}

// 抓取星标
func fetchStars(slug, token string) (int, bool) {
	address := "https://img.shields.io/github/stars/" + slug + ".json"
	accept := "application/json"
	timeout := 5 * time.Second
	if token != "" {
		address = "https://api.github.com/repos/" + slug
		accept = "application/vnd.github+json"
		timeout = 60 * time.Second
	}
	body, _, err := asterism.Request(address, accept, token, nil, timeout)
	if err != nil {
		return 0, false
	}
	if token != "" {
		var data struct {
			Star *int `json:"stargazers_count"`
		}
		if err := json.Unmarshal(body, &data); err != nil || data.Star == nil {
			return 0, false
		}
		return *data.Star, true
	}
	var data struct {
		Value   string `json:"value"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return 0, false
	}
	value := data.Value
	if value == "" {
		value = data.Message
	}
	text := strings.TrimSpace(strings.ToLower(value))
	multiplier := 1.0
	if digits, ok := strings.CutSuffix(text, "k"); ok {
		multiplier = 1000
		text = strings.TrimSpace(digits)
	} else if digits, ok := strings.CutSuffix(text, "m"); ok {
		multiplier = 1_000_000
		text = strings.TrimSpace(digits)
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false
	}
	return int(number*multiplier + 0.5), true
}

// 抓取订阅
func fetchFeeds(store *[]asterism.Version, seen map[string]bool) []string {
	var problems []string
	for _, feed := range []struct{ Name, URL string }{
		{"appinn", "https://www.appinn.com/feed"},
		{"lobsters", "https://lobste.rs/rss"},
		{"hn_show", "https://hnrss.org/show"},
		{"reddit_sh", "https://www.reddit.com/r/selfhosted/.rss"},
		{"aur", "https://aur.archlinux.org/rss/"},
		{"omg_ubuntu", "https://www.omgubuntu.co.uk/feed"},
		{"itsfoss", "https://itsfoss.com/rss/"},
		{"phoronix", "https://www.phoronix.com/rss.php"},
	} {
		raw, err := asterism.FetchFeed(feed.URL, "application/atom+xml, application/rss+xml, text/xml", 30*time.Minute, 60*time.Second)
		if err != nil {
			problems = append(problems, "订阅 "+feed.Name+" 失败："+err.Error())
			continue
		}
		entries := parseFeed(raw)
		for index := range entries {
			entries[index].Kind = "article"
		}
		appendVersions(store, seen, feed.Name, entries)
	}
	return problems
}

// 抓取发行
func fetchReleases(list []*asterism.Software, store *[]asterism.Version, seen map[string]bool) int {
	best := map[string]*asterism.Software{}
	for _, item := range list {
		if !strings.HasPrefix(item.Repository, "github.com/") {
			continue
		}
		current := best[item.Repository]
		if current == nil || item.Score.Niche > current.Score.Niche {
			best[item.Repository] = item
		}
	}
	if len(best) == 0 {
		return 0
	}
	names := make([]string, 0, len(best))
	for name := range best {
		names = append(names, name)
	}
	slices.Sort(names)
	asterism.Log("追踪 %d 个仓库", len(names))

	done := 0
	for start := 0; start < len(names); start += 500 {
		end := min(start+500, len(names))
		batch := names[start:end]
		results := asterism.Parallel(batch, runtime.NumCPU(), func(name string) []asterism.Version {
			raw, err := asterism.FetchFeed("https://"+name+"/releases.atom", "application/atom+xml", 6*time.Hour, 5*time.Second)
			if err != nil {
				return nil
			}
			return parseFeed(raw)
		})
		for index, entries := range results {
			if len(entries) == 0 {
				continue
			}
			done++
			name := batch[index]
			for position := range entries {
				entries[position].Kind = "release"
				entries[position].Repository = name
				if target := best[name]; target != nil {
					entries[position].Software = target.ID
				}
			}
			appendVersions(store, seen, "releases", entries)
		}
		time.Sleep(2 * time.Second)
	}
	return done
}

// 匹配事件
func matchVersions(list []*asterism.Software, store []asterism.Version) int {
	repositories := map[string]int64{}
	for _, item := range list {
		if item.Repository != "" {
			if _, ok := repositories[item.Repository]; !ok {
				repositories[item.Repository] = item.ID
			}
		}
	}
	matched := 0
	pattern := regexp.MustCompile(`github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)`)
	for index := range store {
		if store[index].Software != 0 {
			matched++
			continue
		}
		blob := store[index].Title + " " + store[index].URL + " " + store[index].Summary
		for _, match := range pattern.FindAllStringSubmatch(blob, -1) {
			key := asterism.NormalizeRepository("https://github.com/" + match[1] + "/" + match[2])
			if found, ok := repositories[key]; ok {
				store[index].Software = found
				store[index].Repository = key
				matched++
				break
			}
		}
	}
	return matched
}

// 追加事件
func appendVersions(store *[]asterism.Version, seen map[string]bool, source string, entries []asterism.Version) int {
	added := 0
	for _, entry := range entries {
		if entry.Origin == "" {
			continue
		}
		key := source + "|" + entry.Origin
		if seen[key] {
			continue
		}
		seen[key] = true
		entry.Source = source
		entry.Summary = truncate(entry.Summary, 2000)
		if entry.First == "" {
			entry.First = time.Now().UTC().Format("2006-01-02T15:04:05Z")
		}
		*store = append(*store, entry)
		added++
	}
	return added
}

// 解析订阅
func parseFeed(raw []byte) []asterism.Version {
	feed, err := feedParser.ParseString(string(raw))
	if err != nil {
		return nil
	}
	entries := make([]asterism.Version, 0, len(feed.Items))
	for _, item := range feed.Items {
		link := item.Link
		identifier := item.GUID
		if identifier == "" {
			identifier = link
		}
		if identifier == "" {
			continue
		}
		date := ""
		if item.PublishedParsed != nil {
			date = item.PublishedParsed.UTC().Format("2006-01-02T15:04:05Z")
		}
		if date == "" && item.UpdatedParsed != nil {
			date = item.UpdatedParsed.UTC().Format("2006-01-02T15:04:05Z")
		}
		summary := item.Description
		if summary == "" {
			summary = item.Content
		}
		title := cleanText(item.Title)
		entries = append(entries, asterism.Version{
			Origin: identifier, Title: title, URL: link, Date: date,
			Summary: cleanText(summary), Tag: versionTag(title),
		})
	}
	return entries
}

// 清理文本
func cleanText(text string) string {
	if text == "" {
		return ""
	}
	text = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(text, " ")
	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(text, " "))
}

// 截断文本
func truncate(text string, limit int) string {
	characters := []rune(text)
	if len(characters) <= limit {
		return text
	}
	return string(characters[:limit])
}

// 版本标签
func versionTag(title string) string {
	text := strings.TrimSpace(title)
	if text == "" {
		return ""
	}
	match := regexp.MustCompile(`^[vV]?\d[\w.+\-]*`).FindString(text)
	if len(match) < 2 || len(match) > 40 {
		return ""
	}
	return match
}
