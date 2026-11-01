package extract

import (
	"fmt"
	"math"
	"time"

	asterism "asterism/internal"
)

// 计算评分
func scoreSoftware(item *asterism.Software, now time.Time) {
	sourceSet := map[string]bool{}
	var download int64
	var vote int
	var window string
	for _, record := range item.Records {
		sourceSet[record.Source] = true
		if record.Download > download {
			download = record.Download
			window = record.Window
		}
		vote = max(vote, record.Vote)
	}
	breadth := len(sourceSet)

	voteScore := 0.0
	if vote > 0 {
		voteScore = math.Min(1.0, math.Log10(1+float64(vote))/math.Log10(1+200))
	}

	ageDays := daysSince(latestRelease(item.Records), now)
	recencyScore := 0.0
	switch {
	case ageDays == -1:
	case ageDays <= 90:
		recencyScore = 1.0
	case ageDays <= 365:
		recencyScore = 0.6
	case ageDays <= 730:
		recencyScore = 0.2
	}

	licenseScore := 0.0
	if item.License != "" {
		licenseScore = 1.0
	}

	starExposed := 0.0
	if item.Star > 0 {
		starExposed = exposureLevel(int64(item.Star),
			[]int64{50000, 15000, 5000, 2000}, []float64{1.0, 0.75, 0.4, 0.2})
	}
	downloadExposed := exposureLevel(download,
		[]int64{500000, 150000, 50000, 15000}, []float64{1.0, 0.75, 0.5, 0.25})
	voteExposed := exposureLevel(int64(vote),
		[]int64{2000, 1000, 400}, []float64{0.75, 0.5, 0.25})
	overexposed := max(starExposed, downloadExposed, voteExposed)

	flags := []string{}
	penalty := 0.0
	if ageDays != -1 && ageDays > 730 {
		flags = append(flags, fmt.Sprintf("stale_%dy", ageDays/365))
		penalty = 1.0
	}
	if item.License == "" || item.License == "unknown" || item.License == "proprietary" {
		flags = append(flags, "no_license")
		penalty = max(penalty, 0.4)
	}
	if len(item.Records) == 1 {
		flags = append(flags, "single_source")
	}
	if item.Repository == "" {
		flags = append(flags, "no_public_repo")
		penalty = max(penalty, 0.5)
	}
	if item.Summary == "" {
		flags = append(flags, "no_description")
	}

	niche := 2.0*math.Log1p(float64(breadth)) +
		1.5*math.Log1p(float64(download))/10.0 +
		1.2*voteScore +
		1.0*recencyScore +
		1.0*licenseScore -
		2.5*penalty -
		3.0*overexposed

	item.Score = asterism.Score{
		Niche: asterism.Round(niche, 3), Breadth: breadth, Download: download,
		Window: window, Vote: vote, Age: ageDays, Risk: flags,
	}
}

// 曝光等级
func exposureLevel(value int64, limits []int64, scores []float64) float64 {
	for index, limit := range limits {
		if value >= limit {
			return scores[index]
		}
	}
	return 0
}

// 计算天数
func daysSince(iso string, now time.Time) int {
	parsed, ok := asterism.ParseTime(iso)
	if !ok {
		return -1
	}
	days := int(now.Sub(parsed).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

// 最新发布
func latestRelease(records []*asterism.Package) string {
	latest := ""
	for _, record := range records {
		latest = max(latest, record.Release, record.Change)
	}
	return latest
}
