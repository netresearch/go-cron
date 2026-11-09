// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cron

import (
	"testing"
	"time"
)

// mustZone loads a zone the tests name, or panics.
func mustZone(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func TestZoneName(t *testing.T) {
	for _, tt := range []struct {
		loc  *time.Location
		want string
	}{
		{mustZone("America/New_York"), "New York"},
		{mustZone("Europe/Berlin"), "Berlin"},
		{mustZone("America/Argentina/Buenos_Aires"), "Buenos Aires"},
		{time.UTC, "UTC"},
		{mustZone("Etc/UTC"), "Etc/UTC"},
		{mustZone("Etc/GMT+5"), "Etc/GMT+5"},
		{mustZone("EST5EDT"), "EST5EDT"},
		// A name outside the tz areas is often one of several alike.
		{mustZone("US/Eastern"), "US/Eastern"},
		{mustZone("Canada/Central"), "Canada/Central"},
		{mustZone("Australia/Sydney"), "Sydney"},
		{mustZone("Pacific/Auckland"), "Auckland"},
	} {
		if got := zoneName(tt.loc); got != tt.want {
			t.Errorf("zoneName(%v) = %q, want %q", tt.loc, got, tt.want)
		}
	}
}

func TestOffsetWords(t *testing.T) {
	for d, want := range map[time.Duration]string{
		5 * time.Hour:                   "5 hours",
		time.Hour:                       "1 hour",
		10*time.Hour + 30*time.Minute:   "10 hours 30 minutes",
		30 * time.Minute:                "30 minutes",
		5*time.Hour + 30*time.Minute:    "5 hours 30 minutes",
		-(5*time.Hour + 45*time.Minute): "5 hours 45 minutes",
	} {
		if got := offsetWords(d); got != want {
			t.Errorf("offsetWords(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestShiftWords(t *testing.T) {
	for _, tt := range []struct {
		d           time.Duration
		other, want string
	}{
		{-5 * time.Hour, "UTC", "5 hours behind UTC"},
		{time.Hour, "New York time", "1 hour ahead of New York time"},
		{0, "UTC", "the same as UTC"},
	} {
		if got := shiftWords(tt.d, tt.other); got != tt.want {
			t.Errorf("shiftWords(%v, %q) = %q, want %q", tt.d, tt.other, got, tt.want)
		}
	}
}

func TestSecondWords(t *testing.T) {
	for _, tt := range []struct {
		first, second time.Duration
		other, want   string
	}{
		{-5 * time.Hour, -4 * time.Hour, "UTC", "4 hours"},
		{0, time.Hour, "UTC", "1 hour ahead"},
		{0, -time.Hour, "London time", "1 hour behind"},
		{time.Hour, 0, "UTC", "the same as UTC"},
	} {
		if got := secondWords(tt.first, tt.second, tt.other); got != tt.want {
			t.Errorf("secondWords(%v, %v, %q) = %q, want %q", tt.first, tt.second, tt.other, got, tt.want)
		}
	}
}

func TestZoneYearOf(t *testing.T) {
	utc := func(m time.Month, d, h, minute int) time.Time {
		return time.Date(2027, m, d, h, minute, 0, 0, time.UTC)
	}
	for _, tt := range []struct {
		zone     string
		base     time.Duration
		shifts   []zoneShift
		seasonal bool
	}{
		{"America/New_York", -5 * time.Hour, []zoneShift{{utc(3, 14, 7, 0), -4 * time.Hour}, {utc(11, 7, 6, 0), -5 * time.Hour}}, true},
		{"Asia/Kolkata", 5*time.Hour + 30*time.Minute, nil, false},
		{"Australia/Lord_Howe", 11 * time.Hour, []zoneShift{{utc(4, 3, 15, 0), 10*time.Hour + 30*time.Minute}, {utc(10, 2, 15, 30), 11 * time.Hour}}, true},
	} {
		z := zoneYearOf(mustZone(tt.zone), 2027)
		if z.base != tt.base || len(z.shifts) != len(tt.shifts) || z.seasonal() != tt.seasonal {
			t.Errorf("%s: base %v, %d shifts, seasonal %v; want %v, %d, %v", tt.zone, z.base, len(z.shifts), z.seasonal(), tt.base, len(tt.shifts), tt.seasonal)
			continue
		}
		for i, s := range tt.shifts {
			if got := z.shifts[i]; !got.at.Equal(s.at) || got.off != s.off {
				t.Errorf("%s: shift %d = %v %v, want %v %v", tt.zone, i, got.at.UTC(), got.off, s.at, s.off)
			}
		}
	}
}

func TestUTCShift(t *testing.T) {
	for zone, want := range map[string]string{
		"America/New_York":    "5 hours behind UTC, 4 hours from March to November",
		"Europe/Berlin":       "1 hour ahead of UTC, 2 hours from March to October",
		"Europe/London":       "the same as UTC, 1 hour ahead from March to October",
		"Australia/Lord_Howe": "11 hours ahead of UTC, 10 hours 30 minutes from April to October",
		"Asia/Kolkata":        "5 hours 30 minutes ahead of UTC",
		"Etc/UTC":             "the same as UTC",
	} {
		if got := utcShift(zoneYearOf(mustZone(zone), 2027)); got != want {
			t.Errorf("%s: utcShift = %q, want %q", zone, got, want)
		}
	}
}
