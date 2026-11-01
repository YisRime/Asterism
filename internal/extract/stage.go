package extract

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"

	asterism "asterism/internal"
)

// 守护调度
func Daemon(config asterism.Config, sources []asterism.Source) {
	var busy atomic.Bool
	job := func() {
		if !busy.CompareAndSwap(false, true) {
			return
		}
		defer busy.Store(false)
		if err := Run(config, sources); err != nil {
			asterism.Log("抓取失败：%v", err)
		}
	}
	scheduler := cron.New(cron.WithLogger(cron.DiscardLogger))
	if _, err := scheduler.AddFunc("0 3 * * *", job); err != nil {
		asterism.Log("调度失败：%v", err)
		return
	}
	scheduler.Start()
	if count, err := asterism.CountSoftware(); err == nil && count == 0 {
		job()
	}
}

// 执行抓取
func Run(config asterism.Config, sources []asterism.Source) error {
	known := map[string]bool{}
	names := make([]string, 0, len(sources))
	for _, item := range sources {
		known[item.Name] = true
		names = append(names, item.Name)
	}
	selected := map[string]bool{}
	for _, name := range config.Sources {
		if !known[name] {
			return fmt.Errorf("%q 非可用数据源：%s", name, strings.Join(names, "、"))
		}
		selected[name] = true
	}

	status := asterism.Status{Build: time.Now().UTC().Format("2006-01-02T15:04:05Z")}
	chosen := make([]asterism.Source, 0, len(sources))
	for _, item := range sources {
		if len(selected) > 0 && !selected[item.Name] ||
			len(selected) == 0 && !item.Default {
			continue
		}
		chosen = append(chosen, item)
	}
	for index, item := range chosen {
		start := time.Now()
		parsed, err := item.Parse()
		state := asterism.State{Name: item.Name, Label: item.Label}
		if err == nil && len(parsed) == 0 {
			err = asterism.ErrFailed
		}
		if err != nil {
			asterism.Log("%s 读取失败：%v", item.Name, err)
		} else {
			sort.Slice(parsed, func(left, right int) bool {
				return parsed[left].Origin < parsed[right].Origin
			})
			count, gain, loss, saveErr := asterism.SaveSource(item.Name, parsed)
			state.Count, state.Gain, state.Loss, state.OK = count, gain, loss, saveErr == nil
			if saveErr != nil {
				asterism.Log("%s 入库失败：%v", item.Name, saveErr)
			} else {
				asterism.Log("抓取 %s %d 条记录", item.Name, count)
			}
		}
		state.Span = time.Since(start).Milliseconds()
		status.Source = append(status.Source, state)
		if index < len(chosen)-1 {
			time.Sleep(30 * time.Second)
		}
	}

	if err := rebuild(config.Token, &status); err != nil {
		return err
	}
	if err := asterism.SaveStatus(status); err != nil {
		asterism.Log("保存状态失败：%v", err)
	}
	return nil
}

// 全量重建
func rebuild(token string, status *asterism.Status) error {
	records, err := asterism.LoadPackages()
	if err != nil {
		return fmt.Errorf("读取记录失败：%v", err)
	}
	if len(records) == 0 {
		return fmt.Errorf("读取记录失败：无记录")
	}
	list := buildSoftware(records)

	repositories, err := asterism.LoadRepositories()
	if err != nil {
		return fmt.Errorf("读取仓库失败：%v", err)
	}
	applyStars(list, repositories)
	fillStars(list, repositories, token)

	now := time.Now()
	for _, item := range list {
		scoreSoftware(item, now)
	}
	sort.SliceStable(list, func(left, right int) bool {
		if list[left].Score.Niche != list[right].Score.Niche {
			return list[left].Score.Niche > list[right].Score.Niche
		}
		return list[left].Key < list[right].Key
	})

	coverage := map[string]int{}
	solo := map[string]int{}
	merged := map[string]int{}
	for _, item := range list {
		included := map[string]bool{}
		for _, record := range item.Records {
			included[record.Source] = true
		}
		for name := range included {
			coverage[name]++
			if len(included) == 1 {
				solo[name]++
			} else {
				merged[name]++
			}
		}
	}
	for index := range status.Source {
		state := &status.Source[index]
		state.Software = coverage[state.Name]
		state.Solo = solo[state.Name]
		state.Merged = merged[state.Name]
	}

	baseline := asterism.LoadBaseline(now)
	if err := asterism.SaveSoftware(list, baseline); err != nil {
		return fmt.Errorf("写入数据失败：%v", err)
	}
	if err := asterism.WriteHistory(list, now); err != nil {
		return fmt.Errorf("写入历史失败：%v", err)
	}
	asterism.Log("评分 %d 个软件", len(list))

	events, err := asterism.LoadEvents()
	if err != nil {
		asterism.Log("读取事件失败：%v", err)
		events = nil
	}
	seen := make(map[string]bool, len(events))
	old := make(map[string]struct {
		software   int64
		repository string
	}, len(events))
	for index := range events {
		key := events[index].Source + "|" + events[index].Origin
		old[key] = struct {
			software   int64
			repository string
		}{events[index].Software, events[index].Repository}
		events[index].Software = 0
		seen[key] = true
	}
	for _, problem := range fetchFeeds(&events, seen) {
		asterism.Log("%s", problem)
	}
	fetchReleases(list, &events, seen)
	matchVersions(list, events)
	sort.SliceStable(events, func(left, right int) bool {
		if events[left].Date != events[right].Date {
			return events[left].Date > events[right].Date
		}
		return events[left].Source+"|"+events[left].Origin <
			events[right].Source+"|"+events[right].Origin
	})
	changed := make([]asterism.Version, 0, len(events))
	for _, event := range events {
		key := event.Source + "|" + event.Origin
		previous, ok := old[key]
		if !ok || previous.software != event.Software || previous.repository != event.Repository {
			changed = append(changed, event)
		}
	}
	if err := asterism.SaveEvents(changed); err != nil {
		asterism.Log("保存事件失败：%v", err)
	}
	asterism.Log("收集 %d 条事件", len(events))
	return nil
}
