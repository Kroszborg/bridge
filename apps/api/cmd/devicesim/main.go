// Command devicesim pretends to be a paired Android gateway, for developing
// Bridge without a phone. It pairs with a code from the dashboard, holds the
// gateway WebSocket open, sends heartbeats, and answers send_sms jobs with
// the same reports a phone would (accepted → sent → delivered).
//
//	go run ./cmd/devicesim -code bp_… [-api http://localhost:8080] [-name "Simulated phone"]
//
// It never sends a real SMS.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"bridge/internal/gateway"
)

type pairResponse struct {
	DeviceID     string `json:"device_id"`
	ProjectName  string `json:"project_name"`
	Credential   string `json:"credential"`
	WebSocketURL string `json:"websocket_url"`
}

func main() {
	api := flag.String("api", "http://localhost:8080", "Bridge API URL")
	code := flag.String("code", "", "pairing code from the dashboard (bp_…), or a bridge://pair URI")
	name := flag.String("name", "Simulated phone", "device name")
	interval := flag.Int("interval", 30, "heartbeat interval in seconds")
	flag.DurationVar(&inboundEvery, "inbound-every", 0, "while forwarding is on, simulate an incoming SMS this often (e.g. 20s)")
	flag.Parse()

	token := *code
	if strings.HasPrefix(token, "bridge://") {
		if i := strings.Index(token, "token="); i >= 0 {
			token = strings.SplitN(token[i+6:], "&", 2)[0]
		}
	}
	if !strings.HasPrefix(token, "bp_") {
		fmt.Fprintln(os.Stderr, "pass -code with the pairing code from Devices → Pair device")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	pair, err := pairDevice(ctx, *api, token, *name)
	if err != nil {
		log.Fatalf("pairing failed: %v", err)
	}
	log.Printf("paired as %s in project %q", pair.DeviceID, pair.ProjectName)

	for ctx.Err() == nil {
		if err := run(ctx, pair, *interval); err != nil {
			log.Printf("connection ended: %v; reconnecting in 3s", err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
	}
}

func pairDevice(ctx context.Context, api, token, name string) (*pairResponse, error) {
	install := make([]byte, 8)
	_, _ = rand.Read(install)
	body, _ := json.Marshal(map[string]string{
		"token": token, "installation_id": "sim-" + hex.EncodeToString(install), "name": name,
		"device_model": "Bridge Device Simulator", "android_version": "sim", "app_version": "dev", "app_flavor": "foss",
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(api, "/")+"/v1/device/pair", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct{ Error struct{ Message string } }
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return nil, fmt.Errorf("%s: %s", resp.Status, e.Error.Message)
	}
	var p pairResponse
	return &p, json.NewDecoder(resp.Body).Decode(&p)
}

// inboundEvery is how often to simulate a received SMS; 0 turns it off.
var inboundEvery time.Duration

// sampleInbound is what the simulated phone "receives".
var sampleInbound = []struct{ from, body string }{
	{"AX-HDFCBK", "Your OTP for login is 482913. It is valid for 10 minutes. Do not share it with anyone."},
	{"+919812345678", "Got the parcel, thanks!"},
	{"JD-AMAZON", "Your order ORD-2291 has been delivered."},
	{"+14155550132", "STOP"},
}

func run(ctx context.Context, p *pairResponse, interval int) error {
	ws, resp, err := websocket.Dial(ctx, p.WebSocketURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + p.Credential}},
	})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			log.Fatalf("the server rejected this simulated device (removed?): %v", err)
		}
		return err
	}
	defer ws.CloseNow()
	log.Printf("connected to %s", p.WebSocketURL)

	var writeMu sync.Mutex
	write := func(v any) error {
		frame, _ := json.Marshal(v)
		writeMu.Lock()
		defer writeMu.Unlock()
		return ws.Write(ctx, websocket.MessageText, frame)
	}
	// seen de-duplicates jobs by message ID, as the real app does.
	seen := map[string]bool{}
	var forwarding atomic.Bool

	errs := make(chan error, 1)
	go func() {
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				errs <- err
				return
			}
			var f gateway.Outbound
			_ = json.Unmarshal(data, &f)
			if f.Type != gateway.TypeHeartbeatAck && f.Type != gateway.TypeReportAck {
				log.Printf("← %s", f.Type+" "+f.MessageID+" "+f.Reason)
			}
			switch f.Type {
			case gateway.TypeWelcome, gateway.TypeConfig:
				if f.ForwardInbound != nil {
					forwarding.Store(*f.ForwardInbound)
					log.Printf("  forwarding incoming SMS: %v", *f.ForwardInbound)
				}
			case gateway.TypeUnpaired:
				log.Fatal("removed from the project; exiting")
			case gateway.TypeSendSMS:
				if seen[f.MessageID] {
					continue
				}
				seen[f.MessageID] = true
				go func(f gateway.Outbound) {
					segs, yes := int16(1), true
					log.Printf("  simulating SMS to %s (sim %v): %q", f.To, f.SimSlot, f.Body)
					_ = write(gateway.Inbound{Type: gateway.TypeSMSAccepted, MessageID: f.MessageID})
					time.Sleep(700 * time.Millisecond)
					_ = write(gateway.Inbound{Type: gateway.TypeSMSSent, MessageID: f.MessageID, Segments: &segs})
					time.Sleep(1200 * time.Millisecond)
					_ = write(gateway.Inbound{Type: gateway.TypeSMSDelivery, MessageID: f.MessageID, Delivered: &yes})
				}(f)
			}
		}
	}()

	battery := int16(87)
	charging, network, carrier, sims := true, "wifi", "Simulated Carrier", int16(2)
	var seq int64
	beat := func() error {
		seq++
		if battery > 5 {
			battery--
		}
		frame, _ := json.Marshal(gateway.Inbound{Type: gateway.TypeHeartbeat, Seq: seq, NextIn: interval, Status: &gateway.Status{
			BatteryLevel: &battery, IsCharging: &charging, NetworkType: &network, CarrierName: &carrier, SimCount: &sims,
			Sims: []gateway.SIM{{Slot: 1, Carrier: "Simulated Carrier", DisplayName: "SIM 1"}, {Slot: 2, Carrier: "Second Carrier", DisplayName: "SIM 2"}},
		}})
		log.Printf("→ heartbeat seq=%d battery=%d%%", seq, battery)
		var v json.RawMessage = frame
		return write(v)
	}
	if err := beat(); err != nil {
		return err
	}
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()
	var inbound <-chan time.Time
	if inboundEvery > 0 {
		t := time.NewTicker(inboundEvery)
		defer t.Stop()
		inbound = t.C
	}
	received := 0
	for {
		select {
		case <-inbound:
			if !forwarding.Load() {
				continue
			}
			sample := sampleInbound[received%len(sampleInbound)]
			received++
			slot := int16(1 + received%2)
			log.Printf("→ incoming SMS from %s", sample.from)
			if err := write(gateway.Inbound{
				Type: gateway.TypeSMSReceived, InboundID: fmt.Sprintf("sim-%d-%d", time.Now().UnixNano(), received),
				From: sample.from, Body: sample.body, SimSlot: &slot, ReceivedAt: time.Now().UnixMilli(),
			}); err != nil {
				return err
			}
		case <-ctx.Done():
			_ = ws.Close(websocket.StatusNormalClosure, "simulator stopped")
			return nil
		case err := <-errs:
			return err
		case <-ticker.C:
			if err := beat(); err != nil {
				return err
			}
		}
	}
}
