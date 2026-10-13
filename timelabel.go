package main

import (
	"fmt"
	"time"
)

// Timestamp labels, as GreenBubbles writes them: the nearer a time, the
// shorter its label. In the local time zone, with weeks starting on Monday
// and a 24-hour clock:
//
//	today                          15:04
//	yesterday                      Yesterday 15:04
//	earlier in this week           Monday 15:04
//	another day of this year       Oct 12, 15:04
//	another year                   Oct 12, 2025, 15:04
//
// A time in the future is never "earlier in this week": later today it is
// the time alone, and from tomorrow on the date. Every record keeps its own
// label; the reference's rule for leaving labels out between close messages
// belongs to a chat stream and is not applied.

var (
	weekdayNames = [3][7]string{
		{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
		{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"},
		{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"},
	}
	yesterdayNames = [3]string{"Yesterday", "昨天", "昨天"}
)

// timeLabel is the label of a time as seen at now, in the current language.
func timeLabel(at, now time.Time) string {
	lang := int(langIndex.Load())
	loc := now.Location()
	at = at.In(loc)
	clock := at.Format("15:04")

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	yesterday := today.AddDate(0, 0, -1)
	// Monday is the first day of the week: Sunday is six days after it.
	weekStart := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, loc)

	switch {
	case day.Equal(today):
		return clock
	case day.Equal(yesterday):
		return yesterdayNames[lang] + " " + clock
	case day.Before(today) && !day.Before(weekStart):
		return weekdayNames[lang][at.Weekday()] + " " + clock
	case at.Year() == now.Year():
		if lang == 0 {
			return at.Format("Jan 2") + ", " + clock
		}
		return fmt.Sprintf("%d 月 %d 日 %s", int(at.Month()), at.Day(), clock)
	}
	if lang == 0 {
		return at.Format("Jan 2, 2006") + ", " + clock
	}
	return fmt.Sprintf("%d 年 %d 月 %d 日 %s", at.Year(), int(at.Month()), at.Day(), clock)
}

// untilNextMinute is how long the labels drawn at now stay right: they can
// only change as the minute, and with it possibly the day, turns.
func untilNextMinute(now time.Time) time.Duration {
	return now.Truncate(time.Minute).Add(time.Minute).Sub(now) + 50*time.Millisecond
}
