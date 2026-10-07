// Package id generates prefixed, time-sortable identifiers such as
// "msg_01j9tq4m2xk3v8c7e5r2n0w6yb".
//
// Each ID is a type prefix followed by 26 lowercase Crockford base32 characters
// encoding 48 bits of millisecond timestamp and 80 random bits (the ULID
// layout). IDs created later sort after IDs created earlier, which keeps
// B-tree inserts cheap and makes IDs readable in logs.
package id

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/binary"
	"strings"
	"sync"
	"time"
)

// Prefixes for every entity type. Keep them short and unique.
const (
	User         = "usr"
	Session      = "ses"
	Organization = "org"
	Member       = "mem"
	Project      = "prj"
	APIKey       = "key"
	Device       = "dev"
	PairingToken = "pair"
	Message      = "msg"
	MessageEvent = "mev"
	AuditLog     = "aud"
	Webhook      = "whk"
	Event        = "evt"
	Delivery     = "dlv"
	RequestLog   = "log"
	Invite       = "inv"
	Request      = "req"
)

var encoding = base32.NewEncoding("0123456789abcdefghjkmnpqrstvwxyz").WithPadding(base32.NoPadding)

// New returns a new identifier with the given prefix. IDs from one process
// are strictly increasing, even within the same millisecond, so rows written
// together (such as a message's first timeline events) sort in creation order.
func New(prefix string) string {
	mu.Lock()
	defer mu.Unlock()
	ms := uint64(time.Now().UnixMilli())
	if ms <= lastMS {
		// Same millisecond (or the clock stepped back): continue from the last
		// ID by incrementing its random part.
		ms = lastMS
		for i := len(lastRand) - 1; i >= 0; i-- {
			lastRand[i]++
			if lastRand[i] != 0 {
				break
			}
		}
	} else {
		lastMS = ms
		if _, err := rand.Read(lastRand[:]); err != nil {
			panic("id: crypto/rand failed: " + err.Error())
		}
		lastRand[0] &= 0x7f // leave room to increment without overflowing
	}
	return encode(prefix, ms, lastRand)
}

var (
	mu       sync.Mutex
	lastMS   uint64
	lastRand [10]byte
)

func encode(prefix string, ms uint64, random [10]byte) string {
	var b [16]byte
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], ms)
	copy(b[:6], ts[2:])
	copy(b[6:], random[:])
	return prefix + "_" + encoding.EncodeToString(b[:])
}

// NewAt returns a new identifier with the given prefix and timestamp.
func NewAt(prefix string, t time.Time) string {
	var random [10]byte
	if _, err := rand.Read(random[:]); err != nil {
		panic("id: crypto/rand failed: " + err.Error())
	}
	return encode(prefix, uint64(t.UnixMilli()), random)
}

// HasPrefix reports whether s looks like an ID of the given type.
func HasPrefix(s, prefix string) bool {
	rest, ok := strings.CutPrefix(s, prefix+"_")
	if !ok || len(rest) != 26 {
		return false
	}
	_, err := encoding.DecodeString(rest)
	return err == nil
}
