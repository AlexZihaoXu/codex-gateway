package projects

import (
	"fmt"
	"time"
)

const (
	maximumProjectLimits = 8
	maximumIntervalCount = int64(10_000)
	maximumSafeInteger   = int64(9_007_199_254_740_991)
)

type IntervalUnit string

const (
	IntervalMinute IntervalUnit = "minute"
	IntervalHour   IntervalUnit = "hour"
	IntervalDay    IntervalUnit = "day"
	IntervalWeek   IntervalUnit = "week"
	IntervalMonth  IntervalUnit = "month"
)

type UsageLimit struct {
	TokenLimit    int64
	IntervalCount int64
	IntervalUnit  IntervalUnit
}

type UsageLimitUsage struct {
	UsageLimit
	PeriodStart           time.Time
	PeriodEnd             time.Time
	Requests              int64
	InputTokens           int64
	CachedInputTokens     int64
	OutputTokens          int64
	ReasoningOutputTokens int64
	TotalTokens           int64
}

func validateUsageLimits(limits []UsageLimit) error {
	if len(limits) > maximumProjectLimits {
		return ErrInvalidLimit
	}
	seen := make(map[string]struct{}, len(limits))
	for _, limit := range limits {
		if limit.TokenLimit <= 0 || limit.TokenLimit > maximumSafeInteger ||
			limit.IntervalCount <= 0 || limit.IntervalCount > maximumIntervalCount ||
			!isIntervalUnit(limit.IntervalUnit) {
			return ErrInvalidLimit
		}
		window := fmt.Sprintf("%s:%d", limit.IntervalUnit, limit.IntervalCount)
		if _, exists := seen[window]; exists {
			return ErrInvalidLimit
		}
		seen[window] = struct{}{}
	}
	return nil
}

func usageLimitWindow(at time.Time, limit UsageLimit) (time.Time, time.Time, error) {
	if err := validateUsageLimits([]UsageLimit{limit}); err != nil {
		return time.Time{}, time.Time{}, err
	}
	at = at.UTC()
	switch limit.IntervalUnit {
	case IntervalMinute:
		return fixedDurationWindow(at, time.Duration(limit.IntervalCount)*time.Minute)
	case IntervalHour:
		return fixedDurationWindow(at, time.Duration(limit.IntervalCount)*time.Hour)
	case IntervalDay:
		return fixedDurationWindow(at, time.Duration(limit.IntervalCount)*24*time.Hour)
	case IntervalWeek:
		const weekAnchorUnix = int64(4 * 24 * time.Hour / time.Second)
		seconds := limit.IntervalCount * 7 * 24 * int64(time.Hour/time.Second)
		startUnix := weekAnchorUnix + ((at.Unix()-weekAnchorUnix)/seconds)*seconds
		start := time.Unix(startUnix, 0).UTC()
		return start, start.Add(time.Duration(limit.IntervalCount) * 7 * 24 * time.Hour), nil
	case IntervalMonth:
		monthIndex := int64(at.Year()*12 + int(at.Month()) - 1)
		const monthAnchor = int64(1970 * 12)
		startIndex := monthAnchor + ((monthIndex-monthAnchor)/limit.IntervalCount)*limit.IntervalCount
		start := time.Date(int(startIndex/12), time.Month(startIndex%12)+1, 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, int(limit.IntervalCount), 0), nil
	default:
		return time.Time{}, time.Time{}, ErrInvalidLimit
	}
}

func fixedDurationWindow(at time.Time, duration time.Duration) (time.Time, time.Time, error) {
	seconds := int64(duration / time.Second)
	start := time.Unix((at.Unix()/seconds)*seconds, 0).UTC()
	return start, start.Add(duration), nil
}

func isIntervalUnit(unit IntervalUnit) bool {
	switch unit {
	case IntervalMinute, IntervalHour, IntervalDay, IntervalWeek, IntervalMonth:
		return true
	default:
		return false
	}
}

func copyUsageLimits(limits []UsageLimit) []UsageLimit {
	return append([]UsageLimit(nil), limits...)
}
