package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

func min(arr ...int) int {
	m := arr[0]
	for _, i := range arr[1:] {
		if m > i {
			m = i
		}
	}

	return m
}

func levenStein(x, y string) float64 {
	a, b := []rune(strings.ToUpper(x)), []rune(strings.ToUpper(y))

	arr := make([][]int, len(a)+1)
	for i := range arr {
		arr[i] = make([]int, len(b)+1)

		for j := range arr[i] {
			arr[i][j] = 1e9
		}
	}

	arr[0][0] = 0
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				arr[i][j] = min(arr[i-1][j-1], arr[i-1][j]+1, arr[i][j-1]+1)
			} else {
				arr[i][j] = min(arr[i-1][j-1]+2, arr[i-1][j]+1, arr[i][j-1]+1)
			}
		}
	}

	l := len(a)
	if len(b) > l {
		l = len(b)
	}

	return float64(arr[len(a)][len(b)]) / float64(l)
}

func mostMatchedChannel(parent string, channels []*discordgo.Channel, event *DiscordEvent) (id, name string) {
	category := event.Event.Category

	type pair struct {
		dist     float64
		id, name string
	}

	arr := make([]*pair, 0, len(channels))
	channelMap := map[string]*discordgo.Channel{}

	for i := range channels {
		channelMap[channels[i].ID] = channels[i]
	}

	for i := range channels {
		if parent != "" {
			p, ok := channelMap[channels[i].ParentID]

			if !ok || p.Name != parent {
				continue
			}
		}

		arr = append(arr, &pair{
			dist: levenStein(channels[i].Name, category),
			id:   channels[i].ID,
			name: channels[i].Name,
		})
	}
	sort.Slice(arr, func(i, j int) bool {
		return arr[i].dist < arr[j].dist
	})

	return arr[0].id, arr[0].name
}

func encodeTimestamp(t time.Time) string {
	return t.Local().Format(time.RFC3339)
}

type durationUnit struct {
	Duration time.Duration
	Name     string
}

var durations = []durationUnit{
	{Duration: 7 * 24 * time.Hour, Name: "週間"},
	{Duration: 24 * time.Hour, Name: "日"},
	{Duration: time.Hour, Name: "時間"},
	{Duration: time.Minute, Name: "分"},
	{Duration: time.Second, Name: "秒"},
}

func encodeDuration(t time.Duration) string {
	res := make([]string, 0, len(durations))

	for i := range durations {
		s := t / durations[i].Duration

		if s != 0 {
			res = append(res, fmt.Sprintf("%d%s", s, durations[i].Name))

			t = t % durations[i].Duration
		}
	}
	return strings.Join(res, "")
}

func uniqueEvents(eventss ...[]*Event) []*Event {
	m := map[string]*Event{}
	sum := 0
	for i := range eventss {
		for _, e := range eventss[i] {
			m[e.UID] = e
		}
		sum += len(eventss[i])
	}

	arr := make([]*Event, 0, sum)

	for _, e := range m {
		arr = append(arr, e)
	}

	return arr
}
