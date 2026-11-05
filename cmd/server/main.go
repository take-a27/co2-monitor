// server は MQTT を購読して各デバイスの最新値をメモリに持ち、HTTP で画面と API を提供する。
package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	mqtt "github.com/eclipse/paho.mqtt.golang"

	"co2-monitor/internal/mqttconn"
	"co2-monitor/internal/telemetry"
)

//go:embed index.html
var static embed.FS

type config struct {
	HTTPAddr   string        `env:"HTTP_ADDR" envDefault:":8080"`
	StaleAfter time.Duration `env:"STALE_AFTER" envDefault:"30s"`
	MQTT       mqttconn.Config
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	cfg, err := env.ParseAs[config]()
	if err != nil {
		slog.Error("invalid config", "error", err)
		os.Exit(1)
	}
	if cfg.MQTT.ClientID == "" {
		cfg.MQTT.ClientID = "co2-web-server"
	}

	store := NewStore()

	// 再接続のたびに購読し直す。接続が切れると購読も消えるため。
	subscribe := func(c mqtt.Client) {
		token := c.Subscribe(telemetry.SubscribeTopic, 1, func(_ mqtt.Client, m mqtt.Message) {
			handleMessage(store, m.Payload(), time.Now().UTC())
		})
		token.Wait()
		if err := token.Error(); err != nil {
			slog.Error("subscribe failed", "topic", telemetry.SubscribeTopic, "error", err)
			return
		}
		slog.Info("subscribed", "topic", telemetry.SubscribeTopic)
	}
	client, err := mqttconn.Connect(cfg.MQTT, subscribe)
	if err != nil {
		slog.Error("failed to set up mqtt", "error", err)
		os.Exit(1)
	}

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: newMux(store, cfg.StaleAfter)}
	go func() {
		slog.Info("http server listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server failed", "error", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
	client.Disconnect(1000)
}

func handleMessage(store *Store, payload []byte, receivedAt time.Time) {
	var msg telemetry.Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		slog.Warn("ignored invalid message", "error", err)
		return
	}
	if msg.DeviceID == "" {
		slog.Warn("ignored message without device_id")
		return
	}
	store.Put(msg, receivedAt)
	slog.Info("received", "device_id", msg.DeviceID, "co2_ppm", msg.CO2PPM, "version", msg.AppVersion)
}

func newMux(store *Store, staleAfter time.Duration) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(static))
	mux.HandleFunc("GET /api/latest", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"server_time":         time.Now().UTC(),
			"stale_after_seconds": staleAfter.Seconds(),
			"devices":             store.Snapshot(time.Now(), staleAfter),
		})
	})
	return mux
}

// Store はデバイスごとの最新メッセージだけをメモリに持つ。履歴は持たない。
type Store struct {
	mu     sync.Mutex
	latest map[string]entry
}

type entry struct {
	msg        telemetry.Message
	receivedAt time.Time
}

func NewStore() *Store {
	return &Store{latest: map[string]entry{}}
}

func (s *Store) Put(msg telemetry.Message, receivedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest[msg.DeviceID] = entry{msg: msg, receivedAt: receivedAt}
}

// DeviceStatus は API が返す 1 デバイス分の情報。
type DeviceStatus struct {
	telemetry.Message
	ReceivedAt time.Time `json:"received_at"`
	// Stale は一定時間メッセージが届いていないことを示す。
	Stale bool `json:"stale"`
}

func (s *Store) Snapshot(now time.Time, staleAfter time.Duration) []DeviceStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]DeviceStatus, 0, len(s.latest))
	for _, e := range s.latest {
		out = append(out, DeviceStatus{
			Message:    e.msg,
			ReceivedAt: e.receivedAt,
			Stale:      now.Sub(e.receivedAt) > staleAfter,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceID < out[j].DeviceID })
	return out
}
