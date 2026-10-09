package messaging

import (
	"slices"
	"time"

	"bridge/internal/db/dbq"
)

// candidate is a device considered for a message, with its recent load.
type candidate struct {
	Device         dbq.Device
	RecentSends    int
	OldestInWindow time.Time // zero if nothing was sent in the window
	LastAssigned   time.Time // zero if never used
	DaySends       int       // sends in the last 24 hours, for the daily cap
	OldestInDay    time.Time // zero if nothing was sent in the last 24 hours
}

// candidateFrom reads a DispatchCandidates row. The query returns the epoch
// for "none", which becomes the zero time.
func candidateFrom(r dbq.DispatchCandidatesRow) candidate {
	c := candidate{Device: r.Device, RecentSends: int(r.RecentSends), DaySends: int(r.DaySends)}
	if r.OldestInWindow.Unix() > 0 {
		c.OldestInWindow = r.OldestInWindow
	}
	if r.LastAssigned.Unix() > 0 {
		c.LastAssigned = r.LastAssigned
	}
	if r.OldestInDay.Unix() > 0 {
		c.OldestInDay = r.OldestInDay
	}
	return c
}

// selection is the dispatch decision for one message.
type selection struct {
	Device  *dbq.Device
	RetryIn time.Duration // when no device can take it now
	Offline []dbq.Device  // devices worth waking through push
	Reason  string        // why nothing was chosen
}

// isOnline reports whether a device holds a live connection that recently checked in.
func isOnline(d dbq.Device, now time.Time) bool {
	if d.Status != dbq.DeviceStatusOnline || d.ConnectionID == nil || d.LastSeenAt == nil {
		return false
	}
	allowance := time.Duration(d.HeartbeatInterval)*2*time.Second + 30*time.Second
	return now.Sub(*d.LastSeenAt) <= allowance
}

// choose picks the device for a message: online, under its send limit and
// its daily cap (carriers block SIMs that send too much in a day), preferring phones on a charger and Wi-Fi, then the one that has sent the
// least of its allowance (spreading carrier limits), then the least recently used.
func choose(cands []candidate, pinned *string, now time.Time) selection {
	if pinned != nil {
		cands = slices.DeleteFunc(slices.Clone(cands), func(c candidate) bool { return c.Device.ID != *pinned })
		if len(cands) == 0 {
			return selection{Reason: "device_not_found"}
		}
	}
	if len(cands) == 0 {
		return selection{Reason: "no_device"}
	}

	var available []candidate
	var offline []dbq.Device
	retry := time.Duration(0)
	dailyOnly := true // every online phone that is full is full for the day
	for _, c := range cands {
		if !isOnline(c.Device, now) {
			offline = append(offline, c.Device)
			continue
		}
		underWindow := c.RecentSends < int(c.Device.SendLimitCount)
		underDay := c.DaySends < int(c.Device.DailySendLimit)
		if underWindow && underDay {
			available = append(available, c)
			continue
		}
		// At a limit: capacity frees up when the oldest send leaves its window.
		var wait time.Duration
		dailyOnly = dailyOnly && !underDay
		if !underDay {
			wait = c.OldestInDay.Add(24 * time.Hour).Sub(now)
		} else {
			window := time.Duration(c.Device.SendLimitWindowSeconds) * time.Second
			wait = c.OldestInWindow.Add(window).Sub(now)
		}
		if retry == 0 || wait < retry {
			retry = wait
		}
	}

	if len(available) == 0 {
		reason := "devices_offline"
		if retry > 0 {
			reason = "devices_at_limit"
			if dailyOnly {
				reason = "devices_at_daily_limit"
			}
		} else {
			retry = 30 * time.Second
		}
		return selection{RetryIn: clamp(retry, 5*time.Second, time.Minute), Offline: offline, Reason: reason}
	}

	slices.SortStableFunc(available, func(a, b candidate) int {
		if d := boolRank(isTrue(b.Device.IsCharging)) - boolRank(isTrue(a.Device.IsCharging)); d != 0 {
			return d
		}
		if d := boolRank(isWifi(b.Device)) - boolRank(isWifi(a.Device)); d != 0 {
			return d
		}
		la := float64(a.RecentSends) / float64(a.Device.SendLimitCount)
		lb := float64(b.RecentSends) / float64(b.Device.SendLimitCount)
		if la != lb {
			if la < lb {
				return -1
			}
			return 1
		}
		return a.LastAssigned.Compare(b.LastAssigned)
	})
	d := available[0].Device
	return selection{Device: &d}
}

func clamp(d, lo, hi time.Duration) time.Duration { return max(lo, min(d, hi)) }

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

func isTrue(b *bool) bool { return b != nil && *b }

func isWifi(d dbq.Device) bool { return d.NetworkType != nil && *d.NetworkType == "wifi" }
