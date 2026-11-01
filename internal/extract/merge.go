package extract

import (
	"slices"
	"sort"
	"strings"

	asterism "asterism/internal"
)

// 构建软件
func buildSoftware(records []*asterism.Package) []*asterism.Software {
	roots := buildGroups(records)

	groups := map[string][]*asterism.Package{}
	var order []string
	for _, record := range records {
		root := roots[record.Source+":"+record.Origin]
		if _, ok := groups[root]; !ok {
			order = append(order, root)
		}
		groups[root] = append(groups[root], record)
	}

	softwareList := make([]*asterism.Software, 0, len(order))
	for _, root := range order {
		members := groups[root]
		primary := pickRepresentative(members)

		platformSet := map[string]bool{}
		for _, record := range members {
			for _, platform := range record.Platform {
				platformSet[platform] = true
			}
		}
		var platforms []string
		for _, platform := range []string{"macOS", "Windows", "Linux", "Android", "FreeBSD"} {
			if platformSet[platform] {
				platforms = append(platforms, platform)
			}
		}

		softwareList = append(softwareList, &asterism.Software{
			Key: root, Records: members, Main: primary,
			Summary: primary.Summary, Repository: primary.Repository,
			License: primary.License, Platform: platforms, Star: -1,
		})
	}
	sort.SliceStable(softwareList, func(left, right int) bool { return softwareList[left].Key < softwareList[right].Key })
	return softwareList
}

// 挑选代表
func pickRepresentative(group []*asterism.Package) *asterism.Package {
	var best *asterism.Package
	var bestScore [8]int
	for index, record := range group {
		score := [8]int{}
		if record.Repository != "" {
			score[0] = 1
		}
		name := strings.ToLower(strings.TrimSpace(record.Name))
		if name != "" {
			score[1] = 2
			if slices.ContainsFunc([]string{
				"-appimage", "-xdg", "-wayland", "-x11", "-portable", "-installer",
				" (install)", " (user)", " (portable)",
			}, func(suffix string) bool { return strings.HasSuffix(name, suffix) }) {
				score[1] = 1
			}
			if record.Source == "aur" {
				score[1]--
			}
		}
		switch record.License {
		case "":
			score[2] = 0
		case "proprietary":
			score[2] = 1
		default:
			score[2] = 2
		}
		if record.Summary != "" {
			score[3] = 1
		}
		score[4] = len(record.Summary)
		if record.Version != "" {
			score[5] = 1
		}
		if record.Release != "" || record.Change != "" {
			score[6] = 1
		}
		if record.Link["homepage"] != "" {
			score[7] = 1
		}
		if index == 0 || slices.Compare(bestScore[:], score[:]) < 0 {
			best, bestScore = record, score
		}
	}
	return best
}

// 构建分组
func buildGroups(records []*asterism.Package) map[string]string {
	names := make([]string, len(records))
	for index, record := range records {
		names[index] = asterism.NormalizeName(record.Name)
	}
	families := detectFamily(records, names)

	finder := &unionFind{parent: map[string]string{}}
	repositoryRoot := map[string]string{}
	type entry struct {
		identity   string
		repository string
		name       string
		aliases    []string
	}
	entries := make([]entry, 0, len(records))

	for index, record := range records {
		identity := record.Source + ":" + record.Origin
		finder.find(identity)
		repository := strings.TrimSpace(record.Repository)
		var aliases []string
		for _, alias := range record.Alias {
			if key := asterism.NormalizeName(alias); key != "" {
				aliases = append(aliases, key)
			}
		}
		entries = append(entries, entry{identity, repository, names[index], aliases})
		if repository != "" && !families[repository] {
			if first, ok := repositoryRoot[repository]; ok {
				finder.union(identity, first)
			} else {
				repositoryRoot[repository] = identity
			}
		}
	}

	evidence := map[string]*groupEvidence{}
	lookup := func(root string) *groupEvidence {
		item := evidence[root]
		if item == nil {
			item = &groupEvidence{
				repositories: map[string]bool{}, sites: map[string]bool{}, tokens: map[string]bool{},
				owners: map[string]bool{}, brands: map[string]bool{}, publishers: map[string]bool{},
			}
			evidence[root] = item
		}
		return item
	}
	for index, item := range entries {
		record := records[index]
		group := lookup(finder.find(item.identity))
		if item.repository != "" {
			group.repositories[item.repository] = true
			if owner := ownerName(item.repository); owner != "" {
				group.owners[owner] = true
			}
		}
		if site := siteDomain(record.Link["homepage"]); site != "" {
			group.sites[site] = true
			for _, label := range brandLabels(site) {
				group.brands[label] = true
			}
		}
		union(group.tokens, descTokens(record.Summary))
		if record.Source == "winget" {
			if publisher := wingetPublisher(record.Origin); publisher != "" {
				group.publishers[publisher] = true
			}
		}
	}

	absorb := func(newRoot string, olds ...string) {
		target := lookup(newRoot)
		for _, old := range olds {
			if old == newRoot {
				continue
			}
			item := evidence[old]
			if item == nil {
				continue
			}
			union(target.repositories, item.repositories)
			union(target.sites, item.sites)
			union(target.tokens, item.tokens)
			union(target.owners, item.owners)
			union(target.brands, item.brands)
			union(target.publishers, item.publishers)
			delete(evidence, old)
		}
	}

	nameRoot := map[string]string{}
	for _, item := range entries {
		keys := make([]string, 0, 1+len(item.aliases))
		unique := map[string]bool{}
		for _, key := range append([]string{item.name}, item.aliases...) {
			if key == "" || unique[key] {
				continue
			}
			unique[key] = true
			keys = append(keys, key)
		}
		for _, key := range keys {
			root := finder.find(item.identity)
			previous, seen := nameRoot[key]
			if seen {
				previous = finder.find(previous)
			}
			if !seen || previous == root {
				nameRoot[key] = root
				continue
			}

			left, right := lookup(previous), lookup(root)

			if len(left.repositories) > 0 && len(right.repositories) > 0 &&
				!overlap(left.repositories, right.repositories) {
				continue
			}
			if overlap(left.repositories, families) || overlap(right.repositories, families) {
				continue
			}
			publisherMatch := overlap(left.publishers, right.owners) || overlap(left.publishers, right.brands) ||
				overlap(right.publishers, left.owners) || overlap(right.publishers, left.brands)
			if key != item.name {
				if !publisherMatch {
					continue
				}
			} else if !corroborated(left, right) && !publisherMatch {
				continue
			}

			finder.union(item.identity, previous)
			newRoot := finder.find(item.identity)
			absorb(newRoot, previous, root)
			if item.repository != "" {
				group := lookup(newRoot)
				group.repositories[item.repository] = true
				if owner := ownerName(item.repository); owner != "" {
					group.owners[owner] = true
				}
			}
			nameRoot[key] = newRoot
		}
	}

	result := make(map[string]string, len(entries))
	for _, item := range entries {
		result[item.identity] = finder.find(item.identity)
	}
	return result
}

// 检测家族
func detectFamily(records []*asterism.Package, names []string) map[string]bool {
	repositoryNames := map[string]map[string]bool{}
	for index, record := range records {
		repository := strings.TrimSpace(record.Repository)
		normalized := names[index]
		if repository == "" || normalized == "" {
			continue
		}
		if repositoryNames[repository] == nil {
			repositoryNames[repository] = map[string]bool{}
		}
		repositoryNames[repository][normalized] = true
	}
	families := map[string]bool{}
	for repository, names := range repositoryNames {
		if len(names) >= 6 {
			families[repository] = true
		}
	}
	return families
}

// 证据结构
type groupEvidence struct {
	repositories map[string]bool
	sites        map[string]bool
	tokens       map[string]bool
	owners       map[string]bool
	brands       map[string]bool
	publishers   map[string]bool
}

// 外部佐证
func corroborated(left, right *groupEvidence) bool {
	if overlap(left.sites, right.sites) {
		return true
	}
	if len(left.tokens) == 0 || len(right.tokens) == 0 {
		return false
	}
	common := 0
	for token := range left.tokens {
		if right.tokens[token] {
			common++
		}
	}
	if common >= 2 {
		return true
	}
	return float64(common)/float64(len(left.tokens)+len(right.tokens)-common) >= 0.15
}

// 取仓库主
func ownerName(repository string) string {
	parts := strings.Split(repository, "/")
	if len(parts) < 3 {
		return ""
	}
	return asterism.NormalizeName(parts[1])
}

// 取发布者
func wingetPublisher(id string) string {
	publisher, _, ok := strings.Cut(id, ".")
	if !ok {
		return ""
	}
	name := asterism.NormalizeName(publisher)
	if name == "" || slices.Contains([]string{
		"app", "apps", "the", "open", "opensource", "free", "software",
		"tools", "team", "dev", "code", "www", "org", "com", "github",
		"gitlab", "project", "studio", "labs",
	}, name) {
		return ""
	}
	return name
}

// 取品牌名
func brandLabels(host string) []string {
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return nil
	}
	candidates := []string{parts[len(parts)-2]}
	if len(parts) >= 3 {
		candidates = append(candidates, parts[0])
	}
	labels := make([]string, 0, 2)
	for _, candidate := range candidates {
		label := asterism.NormalizeName(candidate)
		if label != "" && !slices.Contains([]string{
			"app", "apps", "the", "open", "opensource", "free", "software",
			"tools", "team", "dev", "code", "www", "org", "com", "github",
			"gitlab", "project", "studio", "labs",
		}, label) {
			labels = append(labels, label)
		}
	}
	return labels
}

// 提取域名
func siteDomain(homepage string) string {
	address := strings.TrimSpace(strings.ToLower(homepage))
	if address == "" {
		return ""
	}
	if index := strings.Index(address, "://"); index >= 0 {
		address = address[index+3:]
	}
	host := strings.Split(address, "/")[0]
	host = strings.Split(host, "?")[0]
	host = strings.Split(host, ":")[0]
	host = strings.TrimPrefix(host, "www.")
	if host == "" || !strings.Contains(host, ".") {
		return ""
	}
	if slices.Contains([]string{
		"github.com", "gitlab.com", "codeberg.org", "bitbucket.org", "sourceforge.net",
		"git.sr.ht", "sr.ht", "gitea.com", "launchpad.net", "gitlab.gnome.org",
		"invent.kde.org", "salsa.debian.org", "framagit.org", "github.io",
	}, host) || strings.HasSuffix(host, ".github.io") {
		return ""
	}
	return host
}

// 提取实词
func descTokens(summary string) map[string]bool {
	tokens := map[string]bool{}
	for _, entry := range strings.FieldsFunc(strings.ToLower(summary), func(character rune) bool {
		return !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9')
	}) {
		if len(entry) < 3 {
			continue
		}
		if slices.Contains([]string{
			"the", "and", "for", "with", "your", "you", "from", "that", "this",
			"are", "was", "were", "been", "have", "has", "had", "app", "apps",
			"application", "software", "free", "open", "source", "based", "simple",
			"fast", "tool", "tools", "use", "using", "used", "can", "will", "all",
			"new", "make", "makes", "get", "more", "most", "one", "own", "our",
			"its", "not", "but", "also", "any", "how", "what", "when", "where",
			"which", "who", "why", "than", "then", "them", "they", "there", "these",
			"those", "such", "some", "only", "just", "like", "very", "much", "many",
			"other", "into", "over", "out", "about", "version", "support",
			"supports", "supported", "available", "easy", "easily", "lets", "let",
			"allows", "allow", "help", "helps", "provide", "provides", "provided",
			"feature", "features", "includes", "include", "including", "designed",
			"build", "built", "create", "creates", "works", "work", "working",
			"cross", "platform", "platforms", "desktop", "mobile", "linux",
			"windows", "macos", "android",
		}, entry) {
			continue
		}
		tokens[entry] = true
	}
	return tokens
}

// 并查结构
type unionFind struct{ parent map[string]string }

// 查根节点
func (finder *unionFind) find(node string) string {
	if _, ok := finder.parent[node]; !ok {
		finder.parent[node] = node
	}
	for finder.parent[node] != node {
		finder.parent[node] = finder.parent[finder.parent[node]]
		node = finder.parent[node]
	}
	return node
}

// 合并两组
func (finder *unionFind) union(left, right string) {
	leftRoot, rightRoot := finder.find(left), finder.find(right)
	if leftRoot == rightRoot {
		return
	}
	if rightRoot < leftRoot {
		leftRoot, rightRoot = rightRoot, leftRoot
	}
	finder.parent[rightRoot] = leftRoot
}

// 集合相交
func overlap(left, right map[string]bool) bool {
	for key := range left {
		if right[key] {
			return true
		}
	}
	return false
}

// 并入集合
func union(dst, src map[string]bool) {
	for key := range src {
		dst[key] = true
	}
}
