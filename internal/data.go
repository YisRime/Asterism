package asterism

import (
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// 包记录
type Package struct {
	ID          int64
	Source      string
	Origin      string
	Software    int64
	Name        string
	Summary     string
	Description string
	Repository  string
	License     string
	Author      string
	Version     string
	Release     string
	Change      string
	Download    int64
	Window      string
	Vote        int
	Popularity  float64
	Size        int64
	Unit        string
	Platform    []string
	Alias       []string
	Maintainer  []string
	Depend      []string
	Label       map[string][]string
	Link        map[string]string
	Hash        map[string]string
	Extra       map[string]any
	First       string
}

// 仓库记录
type Repository struct {
	Name  string
	Star  int
	Stamp string
	First string
}

// 发布事件
type Version struct {
	Source     string
	Origin     string
	Software   int64
	Repository string
	Tag        string
	Title      string
	URL        string
	Date       string
	Summary    string
	Kind       string
	First      string
}

// 评分结构
type Score struct {
	Niche    float64
	Breadth  int
	Download int64
	Window   string
	Vote     int
	Age      int
}

// 软件结构
type Software struct {
	ID         int64
	Key        string
	Records    []*Package
	Main       *Package
	Summary    string
	Repository string
	License    string
	Platform   []string
	Star       int
	Score      Score
	First      string
}

// 列表条目
type SoftwareRow struct {
	Key        string            `json:"key"`
	Name       string            `json:"name"`
	Version    string            `json:"version,omitempty"`
	Summary    string            `json:"summary"`
	License    string            `json:"license,omitempty"`
	Homepage   string            `json:"homepage,omitempty"`
	Repository string            `json:"repository,omitempty"`
	Link       map[string]string `json:"link,omitempty"`
	Star       *int              `json:"star"`
	Score      float64           `json:"score"`
	Breadth    int               `json:"breadth"`
	Download   int64             `json:"download"`
	Vote       int               `json:"vote"`
	Age        *int              `json:"age"`
	Trend      *float64          `json:"trend,omitempty"`
	Platform   []string          `json:"platform"`
}

// 基线数值
type Baseline struct {
	Score    float64
	Download int64
	Star     int
}

// 数据句柄
var db *sql.DB

// 写入互斥
var writeLock chan struct{}

// 打开数据
func OpenDatabase() error {
	handle, err := sql.Open("sqlite", "data/asterism.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return err
	}
	handle.SetMaxOpenConns(8)
	db = handle
	writeLock = make(chan struct{}, 1)
	writeLock <- struct{}{}
	return migrate()
}

// 关闭数据
func CloseDatabase() {
	if db != nil {
		_ = db.Close()
	}
}

// 建表迁移
func migrate() error {
	_, err := db.Exec(`
PRAGMA user_version = 3;
CREATE TABLE IF NOT EXISTS repository (
	name  TEXT PRIMARY KEY,
	star  INTEGER NOT NULL DEFAULT -1,
	stamp TEXT NOT NULL DEFAULT '',
	first TEXT NOT NULL DEFAULT ''
) STRICT, WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS repository_star ON repository(star DESC);
CREATE TABLE IF NOT EXISTS software (
	id       INTEGER PRIMARY KEY,
	key      TEXT NOT NULL UNIQUE,
	score    REAL NOT NULL DEFAULT 0,
	breadth  INTEGER NOT NULL DEFAULT 0,
	download INTEGER NOT NULL DEFAULT 0,
	window   TEXT NOT NULL DEFAULT '',
	vote     INTEGER NOT NULL DEFAULT 0,
	age      INTEGER NOT NULL DEFAULT -1,
	trend    REAL,
	delta    INTEGER,
	platform TEXT NOT NULL DEFAULT '[]',
	main     INTEGER NOT NULL DEFAULT 0,
	first    TEXT NOT NULL DEFAULT ''
) STRICT;
CREATE INDEX IF NOT EXISTS software_score ON software(score DESC);
CREATE INDEX IF NOT EXISTS software_download ON software(download DESC);
CREATE TABLE IF NOT EXISTS package (
	id          INTEGER PRIMARY KEY,
	source      TEXT NOT NULL,
	origin      TEXT NOT NULL,
	software    INTEGER NOT NULL DEFAULT 0,
	name        TEXT NOT NULL DEFAULT '',
	summary     TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	repository  TEXT NOT NULL DEFAULT '',
	license     TEXT NOT NULL DEFAULT '',
	author      TEXT NOT NULL DEFAULT '',
	version     TEXT NOT NULL DEFAULT '',
	release     TEXT NOT NULL DEFAULT '',
	change      TEXT NOT NULL DEFAULT '',
	download    INTEGER NOT NULL DEFAULT 0,
	window      TEXT NOT NULL DEFAULT '',
	vote        INTEGER NOT NULL DEFAULT 0,
	popularity  REAL NOT NULL DEFAULT 0,
	size        INTEGER NOT NULL DEFAULT 0,
	unit        TEXT NOT NULL DEFAULT '',
	platform    TEXT NOT NULL DEFAULT '[]',
	alias       TEXT NOT NULL DEFAULT '[]',
	maintainer  TEXT NOT NULL DEFAULT '[]',
	depend      TEXT NOT NULL DEFAULT '[]',
	label       TEXT NOT NULL DEFAULT '{}',
	link        TEXT NOT NULL DEFAULT '{}',
	hash        TEXT NOT NULL DEFAULT '{}',
	extra       TEXT NOT NULL DEFAULT '{}',
	first       TEXT NOT NULL DEFAULT '',
	UNIQUE(source, origin)
) STRICT;
CREATE INDEX IF NOT EXISTS package_software ON package(software);
CREATE INDEX IF NOT EXISTS package_repository ON package(repository);
CREATE TABLE IF NOT EXISTS version (
	source     TEXT NOT NULL,
	origin     TEXT NOT NULL,
	software   INTEGER NOT NULL DEFAULT 0,
	repository TEXT NOT NULL DEFAULT '',
	tag        TEXT NOT NULL DEFAULT '',
	title      TEXT NOT NULL DEFAULT '',
	url        TEXT NOT NULL DEFAULT '',
	date       TEXT NOT NULL DEFAULT '',
	summary    TEXT NOT NULL DEFAULT '',
	kind       TEXT NOT NULL DEFAULT '',
	first      TEXT NOT NULL DEFAULT '',
	PRIMARY KEY(source, origin)
) STRICT, WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS version_software ON version(software);
CREATE INDEX IF NOT EXISTS version_repository ON version(repository);
CREATE INDEX IF NOT EXISTS version_date ON version(date DESC);
CREATE TABLE IF NOT EXISTS history (
	day       TEXT NOT NULL,
	software  INTEGER NOT NULL,
	score     REAL NOT NULL DEFAULT 0,
	download  INTEGER NOT NULL DEFAULT 0,
	star      INTEGER NOT NULL DEFAULT -1,
	vote      INTEGER NOT NULL DEFAULT 0,
	breadth   INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY(day, software)
) STRICT, WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS history_software ON history(software, day);
DROP VIEW IF EXISTS software_view;
CREATE VIEW software_view AS
SELECT s.id, s.key, p.name, p.version,
	COALESCE(NULLIF(p.summary, ''), substr(p.description, 1, 200)) AS summary,
	p.description,
	COALESCE(json_extract(p.link, '$.homepage'), '') AS homepage,
	p.link AS links, p.repository, p.license, p.author,
	COALESCE(r.star, -1) AS star,
	s.score, s.breadth, s.download, s.window, s.vote, s.age,
	s.trend, s.delta, s.platform, s.main, s.first
FROM software s
JOIN package p ON p.id = s.main
LEFT JOIN repository r ON r.name = p.repository;`)
	return err
}

// 串行写入
func writeBatch(fn func(tx *sql.Tx) error) error {
	<-writeLock
	defer func() { writeLock <- struct{}{} }()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// 编码数据
func textJSON(value any, empty string) string {
	data, err := json.Marshal(value)
	if err != nil || string(data) == "null" {
		return empty
	}
	return string(data)
}

// 覆盖记录
func SaveSource(source string, packages []*Package) (int, int, int, error) {
	stamp := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	fresh := map[string]*Package{}
	rows := make([]*Package, 0, len(packages))
	for _, item := range packages {
		if item.Origin == "" {
			continue
		}
		if _, exists := fresh[item.Origin]; exists {
			continue
		}
		fresh[item.Origin] = item
		rows = append(rows, item)
	}

	old := map[string]bool{}
	iterator, err := db.Query("SELECT origin FROM package WHERE source=?", source)
	if err != nil {
		return 0, 0, 0, err
	}
	for iterator.Next() {
		var origin string
		if iterator.Scan(&origin) == nil {
			old[origin] = true
		}
	}
	if err := iterator.Err(); err != nil {
		iterator.Close()
		return 0, 0, 0, err
	}
	iterator.Close()

	if len(rows) == 0 {
		return 0, 0, 0, nil
	}
	repos := map[string]bool{}
	for _, item := range rows {
		if item.Repository != "" {
			repos[item.Repository] = true
		}
	}
	gain, loss := 0, 0
	for origin := range fresh {
		if !old[origin] {
			gain++
		}
	}
	var gone []string
	for origin := range old {
		if fresh[origin] == nil {
			loss++
			gone = append(gone, origin)
		}
	}

	err = writeBatch(func(tx *sql.Tx) error {
		stmt, err := tx.Prepare(`INSERT INTO package
			(source, origin, name, summary, description, repository, license, author,
			 version, release, change, download, window, vote, popularity, size, unit,
			 platform, alias, maintainer, depend, label, link, hash, extra, first)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(source, origin) DO UPDATE SET
			name=excluded.name, summary=excluded.summary, description=excluded.description,
			repository=excluded.repository, license=excluded.license, author=excluded.author,
			version=excluded.version, release=excluded.release, change=excluded.change,
			download=excluded.download, window=excluded.window, vote=excluded.vote,
			popularity=excluded.popularity, size=excluded.size, unit=excluded.unit,
			platform=excluded.platform, alias=excluded.alias, maintainer=excluded.maintainer,
			depend=excluded.depend, label=excluded.label, link=excluded.link,
			hash=excluded.hash, extra=excluded.extra`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		repoStmt, err := tx.Prepare(`INSERT OR IGNORE INTO repository (name, first) VALUES (?,?)`)
		if err != nil {
			return err
		}
		defer repoStmt.Close()
		for _, item := range rows {
			first := item.First
			if first == "" {
				first = stamp
			}
			if _, err := stmt.Exec(
				source, item.Origin, item.Name, item.Summary, item.Description,
				item.Repository, item.License, item.Author, item.Version, item.Release,
				item.Change, item.Download, item.Window, item.Vote, item.Popularity,
				item.Size, item.Unit, textJSON(item.Platform, "[]"), textJSON(item.Alias, "[]"),
				textJSON(item.Maintainer, "[]"), textJSON(item.Depend, "[]"), textJSON(item.Label, "{}"),
				textJSON(item.Link, "{}"), textJSON(item.Hash, "{}"), textJSON(item.Extra, "{}"), first,
			); err != nil {
				return err
			}
		}
		for name := range repos {
			if _, err := repoStmt.Exec(name, stamp); err != nil {
				return err
			}
		}
		remove, err := tx.Prepare("DELETE FROM package WHERE source=? AND origin=?")
		if err != nil {
			return err
		}
		defer remove.Close()
		for _, origin := range gone {
			if _, err := remove.Exec(source, origin); err != nil {
				return err
			}
		}
		return nil
	})
	return len(rows), gain, loss, err
}

// 统计软件
func CountSoftware() (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM software").Scan(&count)
	return count, err
}

// 读取包表
func LoadPackages() ([]*Package, error) {
	iterator, err := db.Query(`SELECT id, source, origin, software, name, summary, description,
		repository, license, author, version, release, change, download, window, vote,
		popularity, size, unit, platform, alias, maintainer, depend, label, link, hash,
		extra, first FROM package ORDER BY source, origin`)
	if err != nil {
		return nil, err
	}
	defer iterator.Close()
	var out []*Package
	for iterator.Next() {
		item := &Package{}
		var platform, alias, maintainer, depend, label, link, hash, extra string
		if err := iterator.Scan(&item.ID, &item.Source, &item.Origin, &item.Software,
			&item.Name, &item.Summary, &item.Description, &item.Repository, &item.License,
			&item.Author, &item.Version, &item.Release, &item.Change, &item.Download,
			&item.Window, &item.Vote, &item.Popularity, &item.Size, &item.Unit,
			&platform, &alias, &maintainer, &depend, &label, &link, &hash, &extra,
			&item.First); err != nil {
			return nil, err
		}
		item.Platform = decodeJSON[[]string](platform)
		item.Alias = decodeJSON[[]string](alias)
		item.Maintainer = decodeJSON[[]string](maintainer)
		item.Depend = decodeJSON[[]string](depend)
		item.Label = decodeJSON[map[string][]string](label)
		item.Link = decodeJSON[map[string]string](link)
		item.Hash = decodeJSON[map[string]string](hash)
		item.Extra = decodeJSON[map[string]any](extra)
		out = append(out, item)
	}
	return out, iterator.Err()
}

// 重建软件
func SaveSoftware(list []*Software, baseline map[string]Baseline) error {
	oldKey := map[int64]string{}
	oldFirst := map[int64]string{}
	keyIndex := map[string]int64{}
	var maxIndex int64
	iterator, err := db.Query("SELECT id, key, first FROM software")
	if err != nil {
		return err
	}
	for iterator.Next() {
		var id int64
		var key, first string
		if iterator.Scan(&id, &key, &first) == nil {
			oldKey[id] = key
			oldFirst[id] = first
			keyIndex[key] = id
			if id > maxIndex {
				maxIndex = id
			}
		}
	}
	if err := iterator.Err(); err != nil {
		iterator.Close()
		return err
	}
	iterator.Close()

	used := map[int64]bool{}
	for _, item := range list {
		id := int64(0)
		for _, record := range item.Records {
			if record.Software != 0 && oldKey[record.Software] != "" && !used[record.Software] {
				id = record.Software
				break
			}
		}
		if id == 0 {
			if found := keyIndex[item.Key]; found != 0 && !used[found] {
				id = found
			}
		}
		if id == 0 {
			maxIndex++
			id = maxIndex
		}
		used[id] = true
		item.ID = id
		if oldKey[id] != "" {
			item.Key = oldKey[id]
		}
		if oldFirst[id] != "" {
			item.First = oldFirst[id]
		}
	}

	stamp := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	err = writeBatch(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM software"); err != nil {
			return err
		}
		stmt, err := tx.Prepare(`INSERT INTO software
			(id, key, score, breadth, download, window, vote, age, trend, delta,
			 platform, main, first) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		if _, err := tx.Exec("DROP TABLE IF EXISTS link"); err != nil {
			return err
		}
		if _, err := tx.Exec("CREATE TEMP TABLE link (id INTEGER PRIMARY KEY, software INTEGER NOT NULL)"); err != nil {
			return err
		}
		link, err := tx.Prepare("INSERT OR REPLACE INTO link (id, software) VALUES (?,?)")
		if err != nil {
			return err
		}
		defer link.Close()
		for _, item := range list {
			var trend *float64
			var delta *int64
			if base, ok := baseline[item.Key]; ok {
				value := Round(item.Score.Niche-base.Score, 3)
				change := item.Score.Download - base.Download
				trend, delta = &value, &change
			}
			first := item.First
			if first == "" {
				first = stamp
			}
			if _, err := stmt.Exec(item.ID, item.Key, item.Score.Niche, item.Score.Breadth,
				item.Score.Download, item.Score.Window, item.Score.Vote, item.Score.Age,
				trend, delta, textJSON(item.Platform, "[]"),
				item.Main.ID, first); err != nil {
				return err
			}
			for _, record := range item.Records {
				if _, err := link.Exec(record.ID, item.ID); err != nil {
					return err
				}
			}
		}
		if _, err := tx.Exec(`UPDATE package
			SET software = (SELECT software FROM link WHERE link.id = package.id)
			WHERE id IN (SELECT id FROM link)`); err != nil {
			return err
		}
		_, err = tx.Exec("DROP TABLE link")
		return err
	})
	if err != nil {
		return err
	}
	totalCache.Range(func(key, _ any) bool {
		totalCache.Delete(key)
		return true
	})
	return nil
}

// 列表条件
func listWhere(queryText, platforms string) (string, []any) {
	where, arguments := []string{"1=1"}, []any{}
	if queryText != "" {
		like := "%" + strings.ToLower(queryText) + "%"
		where = append(where, "(lower(name) LIKE ? OR lower(summary) LIKE ? OR "+
			"lower(license) LIKE ? OR lower(author) LIKE ? OR lower(repository) LIKE ?)")
		for range 5 {
			arguments = append(arguments, like)
		}
	}
	if platforms != "" {
		for _, platform := range strings.Split(platforms, ",") {
			if platform = strings.TrimSpace(platform); platform != "" {
				where = append(where, "EXISTS (SELECT 1 FROM json_each(platform) WHERE value = ?)")
				arguments = append(arguments, platform)
			}
		}
	}
	return strings.Join(where, " AND "), arguments
}

// 计数缓存
var totalCache sync.Map

// 统计列表
func QueryTotal(queryText, platforms string) (int, error) {
	queryText, platforms = strings.TrimSpace(queryText), strings.TrimSpace(platforms)
	key := queryText + "\x1f" + platforms
	if value, ok := totalCache.Load(key); ok {
		return value.(int), nil
	}
	condition, arguments := listWhere(queryText, platforms)
	var total int
	if err := db.QueryRow("SELECT COUNT(*) FROM software_view WHERE "+condition,
		arguments...).Scan(&total); err != nil {
		return 0, err
	}
	totalCache.Store(key, total)
	return total, nil
}

// 查询列表
func QueryList(queryText, platforms, sort string, offset, limit int) ([]SoftwareRow, error) {
	condition, arguments := listWhere(strings.TrimSpace(queryText), platforms)
	order, ok := map[string]string{
		"score": "score DESC", "trend": "trend DESC", "star": "star DESC",
		"download": "download DESC", "vote": "vote DESC",
		"fresh": "CASE WHEN age < 0 THEN 1 ELSE 0 END, age ASC",
		"name":  "name COLLATE NOCASE ASC",
	}[sort]
	if !ok {
		order = "score DESC"
	}

	iterator, err := db.Query(`SELECT key, name, version, summary, license, star,
		score, breadth, download, vote, age, trend, platform, homepage, repository, links
		FROM software_view WHERE `+condition+" ORDER BY "+order+" LIMIT ? OFFSET ?",
		append(arguments, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer iterator.Close()

	items := make([]SoftwareRow, 0, limit)
	for iterator.Next() {
		var row SoftwareRow
		var star, age, trend any
		var platform, links string
		if err := iterator.Scan(&row.Key, &row.Name, &row.Version, &row.Summary,
			&row.License, &star, &row.Score, &row.Breadth, &row.Download, &row.Vote,
			&age, &trend, &platform, &row.Homepage, &row.Repository, &links); err != nil {
			return nil, err
		}
		row.Platform = decodeJSON[[]string](platform)
		row.Link = decodeJSON[map[string]string](links)
		if value, ok := star.(int64); ok && value > -1 {
			number := int(value)
			row.Star = &number
		}
		if value, ok := age.(int64); ok && value > -1 {
			number := int(value)
			row.Age = &number
		}
		if value, ok := trend.(float64); ok {
			row.Trend = &value
		}
		items = append(items, row)
	}
	return items, iterator.Err()
}

// 查询详情
func GetInfo(key string, sources []Source) (map[string]any, error) {
	var item struct {
		ID                                int64
		Key, Name, Version, Summary       string
		Description, Homepage, Repository string
		License, Author                   string
		Star                              any
		Score                             float64
		Breadth                           int
		Download                          int64
		Window                            string
		Vote                              int
		Age, Trend, Delta                 any
		Platform                          string
		Main                              int64
		First                             string
	}
	row := db.QueryRow(`SELECT id, key, name, version, summary, description, homepage,
		repository, license, author, star, score, breadth, download, window, vote,
		age, trend, delta, platform, main, first FROM software_view WHERE key=?`, key)
	if err := row.Scan(&item.ID, &item.Key, &item.Name, &item.Version, &item.Summary,
		&item.Description, &item.Homepage, &item.Repository, &item.License, &item.Author,
		&item.Star, &item.Score, &item.Breadth, &item.Download, &item.Window, &item.Vote,
		&item.Age, &item.Trend, &item.Delta, &item.Platform, &item.Main,
		&item.First); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	labels := map[string][]string{}
	labelSet := map[string]map[string]bool{}
	maintainers := []string{}
	maintainerSet := map[string]bool{}
	best := map[string]string{}

	add := func(set map[string]bool, into *[]string, values []string) {
		for _, value := range values {
			if value != "" && !set[value] {
				set[value] = true
				*into = append(*into, value)
			}
		}
	}
	addGroup := func(kind string, values []string) {
		if len(values) == 0 {
			return
		}
		if labelSet[kind] == nil {
			labelSet[kind] = map[string]bool{}
		}
		for _, value := range values {
			if value != "" && !labelSet[kind][value] {
				labelSet[kind][value] = true
				labels[kind] = append(labels[kind], value)
			}
		}
	}

	iterator, err := db.Query(`SELECT source, origin, maintainer, label
		FROM package WHERE software=? ORDER BY source, origin`, item.ID)
	if err != nil {
		return nil, err
	}
	for iterator.Next() {
		var source, origin string
		var maintainer, label string
		if err := iterator.Scan(&source, &origin,
			&maintainer, &label); err != nil {
			iterator.Close()
			return nil, err
		}
		add(maintainerSet, &maintainers, decodeJSON[[]string](maintainer))
		group := decodeJSON[map[string][]string](label)
		for kind, values := range group {
			addGroup(kind, values)
		}
		if current, exists := best[source]; !exists || len(origin) < len(current) {
			best[source] = origin
		}
	}
	if err := iterator.Err(); err != nil {
		iterator.Close()
		return nil, err
	}
	iterator.Close()

	installs := []map[string]any{}
	for _, source := range sources {
		origin, ok := best[source.Name]
		if !ok {
			continue
		}
		one := map[string]any{"source": source.Name, "label": source.Label, "id": origin}
		if source.Command != "" {
			one["command"] = strings.ReplaceAll(source.Command, "{id}", origin)
		}
		if source.InstallURL != "" {
			one["url"] = strings.ReplaceAll(source.InstallURL, "{id}", origin)
		}
		installs = append(installs, one)
	}

	events := []map[string]any{}
	eventIterator, err := db.Query(`SELECT source, title, url, date, summary, kind, tag
		FROM version WHERE software=? ORDER BY date DESC`, item.ID)
	if err != nil {
		return nil, err
	}
	for eventIterator.Next() {
		var source, title, url, date, summary, kind, tag string
		if err := eventIterator.Scan(&source, &title, &url, &date, &summary, &kind, &tag); err != nil {
			eventIterator.Close()
			return nil, err
		}
		events = append(events, map[string]any{
			"source": source, "title": title, "url": url, "date": date,
			"summary": summary, "kind": kind, "tag": tag,
		})
	}
	if err := eventIterator.Err(); err != nil {
		eventIterator.Close()
		return nil, err
	}
	eventIterator.Close()

	var depend, change, window string
	var size int64
	extraIterator, err := db.Query(`SELECT depend, change, size, window FROM package WHERE software=? ORDER BY source`, item.ID)
	if err != nil {
		return nil, err
	}
	for extraIterator.Next() {
		var dependText, changeText, windowText string
		var sizeValue int64
		if err := extraIterator.Scan(&dependText, &changeText, &sizeValue, &windowText); err != nil {
			extraIterator.Close()
			return nil, err
		}
		if depend == "" && len(dependText) > 2 {
			depend = dependText
		}
		if change == "" {
			change = changeText
		}
		if sizeValue > size {
			size = sizeValue
		}
		if window == "" {
			window = windowText
		}
	}
	if err := extraIterator.Err(); err != nil {
		extraIterator.Close()
		return nil, err
	}
	extraIterator.Close()
	if depend == "" {
		depend = "[]"
	}

	return map[string]any{
		"key": item.Key, "name": item.Name, "version": item.Version,
		"summary": item.Summary, "description": item.Description,
		"homepage": item.Homepage, "repository": item.Repository,
		"license": item.License, "author": item.Author,
		"star": nullable(item.Star),
		"change": change, "size": size,
		"window": window,
		"platform": decodeJSON[[]string](item.Platform),
		"label":    labels, "maintainer": maintainers,
		"depend":   decodeJSON[[]string](depend),
		"install": installs, "event": events,
	}, nil
}

// 空值处理
func nullable(value any) any {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case int64:
		if typed <= -1 {
			return nil
		}
		return typed
	case float64:
		return typed
	}
	return nil
}

// 写入历史
func WriteHistory(list []*Software, now time.Time) error {
	day := now.Format("2006-01-02")
	return writeBatch(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM history WHERE day=?", day); err != nil {
			return err
		}
		stmt, err := tx.Prepare(`INSERT OR REPLACE INTO history
			(day, software, score, download, star, vote, breadth) VALUES (?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		written := map[int64]bool{}
		for _, item := range list {
			if len(written) >= 5000 {
				break
			}
			if item.ID == 0 || written[item.ID] {
				continue
			}
			written[item.ID] = true
			if _, err := stmt.Exec(day, item.ID, item.Score.Niche,
				item.Score.Download, item.Star, item.Score.Vote, item.Score.Breadth); err != nil {
				return err
			}
		}
		_, err = tx.Exec("DELETE FROM history WHERE day < ?",
			now.AddDate(0, 0, -90).Format("2006-01-02"))
		return err
	})
}

// 读取基线
func LoadBaseline(now time.Time) map[string]Baseline {
	var day string
	if err := db.QueryRow(`SELECT day FROM history WHERE day < ?
		ORDER BY ABS(julianday(day) - julianday(?)) LIMIT 1`,
		now.Format("2006-01-02"), now.AddDate(0, 0, -7).Format("2006-01-02")).Scan(&day); err != nil {
		return map[string]Baseline{}
	}
	iterator, err := db.Query(`SELECT s.key, h.score, h.download, h.star
		FROM history h JOIN software s ON s.id = h.software WHERE h.day=?`, day)
	if err != nil {
		return map[string]Baseline{}
	}
	defer iterator.Close()
	store := map[string]Baseline{}
	for iterator.Next() {
		var key string
		var baseline Baseline
		if err := iterator.Scan(&key, &baseline.Score, &baseline.Download, &baseline.Star); err != nil {
			continue
		}
		store[key] = baseline
	}
	if err := iterator.Err(); err != nil {
		return map[string]Baseline{}
	}
	return store
}

// 读取仓库
func LoadRepositories() (map[string]*Repository, error) {
	iterator, err := db.Query("SELECT name, star, stamp, first FROM repository")
	if err != nil {
		return nil, err
	}
	defer iterator.Close()
	store := map[string]*Repository{}
	for iterator.Next() {
		item := &Repository{}
		if err := iterator.Scan(&item.Name, &item.Star, &item.Stamp, &item.First); err != nil {
			return nil, err
		}
		store[item.Name] = item
	}
	return store, iterator.Err()
}

// 保存星标
func SaveStar(name string, star int, stamp string) error {
	_, err := db.Exec(`INSERT INTO repository (name, star, stamp, first) VALUES (?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET star=excluded.star, stamp=excluded.stamp`,
		name, star, stamp, stamp)
	return err
}

// 汇总统计
func LoadSummary() (map[string]any, error) {
	var software, multi, withStar, repos, scored, fresh, licenses int
	var download int64
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(download),0),
		COALESCE(SUM(CASE WHEN breadth > 1 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN star > -1 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN repository != '' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN score >= 6 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN age >= 0 AND age < 7 THEN 1 ELSE 0 END),0),
		COUNT(DISTINCT CASE WHEN license != '' THEN license END)
		FROM software_view`).Scan(&software, &download, &multi, &withStar,
		&repos, &scored, &fresh, &licenses); err != nil {
		return nil, err
	}
	var packages, versions, matched, sources, days int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM package),
		(SELECT COUNT(*) FROM version),
		(SELECT COUNT(*) FROM version WHERE software != 0),
		(SELECT COUNT(DISTINCT source) FROM package),
		(SELECT COUNT(DISTINCT day) FROM history)`).Scan(
		&packages, &versions, &matched, &sources, &days); err != nil {
		return nil, err
	}
	status := LoadStatus()
	platforms, err := platformCounts(status.Build)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"software": software, "package": packages, "source": sources,
		"version": versions, "matched": matched,
		"with_star": withStar, "multi": multi, "download": download,
		"license": licenses, "repo": repos, "scored": scored,
		"fresh": fresh, "history_day": days,
		"platform": platforms, "build": status.Build,
		"source_stat": status.Source,
	}, nil
}

// 平台分布
var platformCache struct {
	sync.Mutex
	build  string
	counts map[string]int
}

func platformCounts(build string) (map[string]int, error) {
	platformCache.Lock()
	defer platformCache.Unlock()
	if platformCache.counts != nil && platformCache.build == build {
		return platformCache.counts, nil
	}
	counts := map[string]int{}
	rows, err := db.Query(`SELECT value, COUNT(*) FROM software_view, json_each(platform) GROUP BY value`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return nil, err
		}
		counts[name] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	platformCache.build = build
	platformCache.counts = counts
	return counts, nil
}

// 读取状态
func LoadStatus() Status {
	var status Status
	raw, err := os.ReadFile("data/status.json")
	if err != nil {
		return status
	}
	_ = json.Unmarshal(raw, &status)
	return status
}

// 写入状态
func SaveStatus(status Status) error {
	raw, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	path := "data/status.json"
	if err := os.WriteFile(path+".tmp", raw, 0o644); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// 解码数据
func decodeJSON[Value any](raw string) Value {
	var value Value
	if raw == "" {
		return value
	}
	_ = json.Unmarshal([]byte(raw), &value)
	return value
}

// 读取事件
func LoadEvents() ([]Version, error) {
	iterator, err := db.Query(`SELECT source, origin, software, repository, tag, title,
		url, date, summary, kind, first FROM version ORDER BY date DESC`)
	if err != nil {
		return nil, err
	}
	defer iterator.Close()
	var out []Version
	for iterator.Next() {
		var item Version
		if err := iterator.Scan(&item.Source, &item.Origin, &item.Software,
			&item.Repository, &item.Tag, &item.Title, &item.URL, &item.Date,
			&item.Summary, &item.Kind, &item.First); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, iterator.Err()
}

// 保存事件
func SaveEvents(events []Version) error {
	if len(events) == 0 {
		return nil
	}
	return writeBatch(func(tx *sql.Tx) error {
		stmt, err := tx.Prepare(`INSERT INTO version
			(source, origin, software, repository, tag, title, url, date, summary, kind, first)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(source, origin) DO UPDATE SET
			software=excluded.software, repository=excluded.repository, tag=excluded.tag,
			title=excluded.title, url=excluded.url, date=excluded.date,
			summary=excluded.summary, kind=excluded.kind`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, item := range events {
			if _, err := stmt.Exec(item.Source, item.Origin, item.Software,
				item.Repository, item.Tag, item.Title, item.URL, item.Date,
				item.Summary, item.Kind, item.First); err != nil {
				return err
			}
		}
		return nil
	})
}
