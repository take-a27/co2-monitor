package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleMessageAndStale(t *testing.T) {
	store := NewStore()
	t0 := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

	handleMessage(store, []byte(`{"device_id":"pi-01","co2_ppm":800,"temperature_c":23.4,"humidity_pct":40.1,"app_version":"1.0.0","mock":true}`), t0)
	handleMessage(store, []byte(`not json`), t0)
	handleMessage(store, []byte(`{"co2_ppm":1}`), t0)

	fresh := store.Snapshot(t0.Add(10*time.Second), 30*time.Second)
	if len(fresh) != 1 {
		t.Fatalf("devices = %d, want 1", len(fresh))
	}
	if d := fresh[0]; d.DeviceID != "pi-01" || d.CO2PPM != 800 || d.AppVersion != "1.0.0" || !d.Mock || d.Stale {
		t.Errorf("unexpected status: %+v", d)
	}

	old := store.Snapshot(t0.Add(31*time.Second), 30*time.Second)
	if !old[0].Stale {
		t.Error("expected stale after 31s")
	}
}

func TestDuplicateMessageOverwrites(t *testing.T) {
	store := NewStore()
	msg := []byte(`{"device_id":"pi-01","co2_ppm":800,"app_version":"1.0.0"}`)
	now := time.Now()
	handleMessage(store, msg, now)
	handleMessage(store, msg, now)

	if got := store.Snapshot(now, time.Minute); len(got) != 1 {
		t.Fatalf("devices = %d, want 1", len(got))
	}
}

func TestLatestAPI(t *testing.T) {
	store := NewStore()
	handleMessage(store, []byte(`{"device_id":"pi-01","co2_ppm":650,"app_version":"1.0.1"}`), time.Now())

	rec := httptest.NewRecorder()
	newMux(store, 30*time.Second).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/latest", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Devices []DeviceStatus `json:"devices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Devices) != 1 || body.Devices[0].AppVersion != "1.0.1" {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}
}

func TestIndexPage(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux(NewStore(), time.Minute).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}
