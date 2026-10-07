package messaging

import (
	"testing"
	"time"

	"bridge/internal/db/dbq"
)

func device(id string, online, charging bool, network string, limit int32) dbq.Device {
	now := time.Now()
	conn := "conn_" + id
	d := dbq.Device{
		ID: id, Status: dbq.DeviceStatusOffline, HeartbeatInterval: 60, IsCharging: &charging, NetworkType: &network,
		SendLimitCount: limit, SendLimitWindowSeconds: 1800,
	}
	if online {
		d.Status, d.ConnectionID, d.LastSeenAt = dbq.DeviceStatusOnline, &conn, &now
	}
	return d
}

func TestChoosePrefersHealthyLeastLoadedDevice(t *testing.T) {
	now := time.Now()
	cands := []candidate{
		{Device: device("battery_cell", true, false, "cellular", 30)},
		{Device: device("charging_wifi_busy", true, true, "wifi", 30), RecentSends: 20},
		{Device: device("charging_wifi_idle", true, true, "wifi", 30), RecentSends: 2},
		{Device: device("offline", false, true, "wifi", 30)},
	}
	if got := choose(cands, nil, now); got.Device == nil || got.Device.ID != "charging_wifi_idle" {
		t.Fatalf("picked %+v", got)
	}
}

func TestChooseSpreadsLoadByShareOfLimit(t *testing.T) {
	now := time.Now()
	cands := []candidate{
		{Device: device("small", true, true, "wifi", 10), RecentSends: 5},   // 50% used
		{Device: device("large", true, true, "wifi", 100), RecentSends: 20}, // 20% used
	}
	if got := choose(cands, nil, now); got.Device.ID != "large" {
		t.Fatalf("picked %s", got.Device.ID)
	}
}

func TestChooseWaitsWhenAllAtLimit(t *testing.T) {
	now := time.Now()
	cands := []candidate{{Device: device("full", true, true, "wifi", 5), RecentSends: 5, OldestInWindow: now.Add(-1790 * time.Second)}}
	got := choose(cands, nil, now)
	if got.Device != nil || got.Reason != "devices_at_limit" || got.RetryIn < 5*time.Second || got.RetryIn > 15*time.Second {
		t.Fatalf("got %+v", got)
	}
}

func TestChooseOfflineAndStale(t *testing.T) {
	now := time.Now()
	stale := device("stale", true, true, "wifi", 30)
	old := now.Add(-10 * time.Minute)
	stale.LastSeenAt = &old // the server has not swept it yet, but it stopped checking in
	got := choose([]candidate{{Device: stale}, {Device: device("off", false, true, "wifi", 30)}}, nil, now)
	if got.Device != nil || got.Reason != "devices_offline" || len(got.Offline) != 2 || got.RetryIn != 30*time.Second {
		t.Fatalf("got %+v", got)
	}
}

func TestChoosePinnedDevice(t *testing.T) {
	now := time.Now()
	cands := []candidate{{Device: device("a", true, true, "wifi", 30)}, {Device: device("b", true, false, "cellular", 30)}}
	pin := "b"
	if got := choose(cands, &pin, now); got.Device == nil || got.Device.ID != "b" {
		t.Fatalf("pinned: %+v", got)
	}
	missing := "zzz"
	if got := choose(cands, &missing, now); got.Reason != "device_not_found" {
		t.Fatalf("missing pin: %+v", got)
	}
	if got := choose(nil, nil, now); got.Reason != "no_device" {
		t.Fatalf("no devices: %+v", got)
	}
}

func TestNormalizeE164(t *testing.T) {
	for in, want := range map[string]string{
		"+91 98765-43210":   "+919876543210",
		"+1 (415) 555-0132": "+14155550132",
		"+44 7700 900123":   "+447700900123",
	} {
		got, err := NormalizeE164(in)
		if err != nil || got != want {
			t.Errorf("NormalizeE164(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"9876543210", "+1", "+999999", "hello", ""} {
		if _, err := NormalizeE164(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
