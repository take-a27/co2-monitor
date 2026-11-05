// device はラズパイ上で動き、センサーの値を一定間隔で MQTT に送信する。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	mqtt "github.com/eclipse/paho.mqtt.golang"

	"co2-monitor/internal/mqttconn"
	"co2-monitor/internal/sensor"
	"co2-monitor/internal/telemetry"
)

// version はビルド時に -ldflags "-X main.version=1.0.1" で埋め込む。
var version = "dev"

type Sensor interface {
	Read(ctx context.Context) (sensor.Reading, error)
	Close() error
}

type config struct {
	DeviceID  string        `env:"DEVICE_ID,required"`
	Sensor    string        `env:"SENSOR" envDefault:"mock"` // scd41 または mock
	I2CDevice string        `env:"I2C_DEVICE" envDefault:"/dev/i2c-1"`
	Interval  time.Duration `env:"INTERVAL" envDefault:"5s"`
	MQTT      mqttconn.Config
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	cfg, err := env.ParseAs[config]()
	if err != nil {
		slog.Error("invalid config", "error", err)
		os.Exit(1)
	}
	if cfg.MQTT.ClientID == "" {
		cfg.MQTT.ClientID = cfg.DeviceID
	}

	slog.Info("starting device", "version", version, "device_id", cfg.DeviceID, "sensor", cfg.Sensor, "interval", cfg.Interval)

	s, err := openSensor(cfg.Sensor, cfg.I2CDevice)
	if err != nil {
		slog.Error("failed to open sensor", "error", err)
		os.Exit(1)
	}

	client, err := mqttconn.Connect(cfg.MQTT, nil)
	if err != nil {
		slog.Error("failed to set up mqtt", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	topic := telemetry.Topic(cfg.DeviceID)
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		// SIGTERM がきたらシャットダウンする
		case <-ctx.Done():
			slog.Info("shutting down")
			if err := s.Close(); err != nil {
				slog.Warn("failed to close sensor", "error", err)
			}
			client.Disconnect(1000)
			slog.Info("stopped")
			return
		// interval おきにセンサーのコマンドを叩いて、メトリクスを収集する
		case <-ticker.C:
			measureAndPublish(ctx, client, s, topic, cfg.DeviceID, cfg.Sensor == "mock")
		}
	}
}

func measureAndPublish(ctx context.Context, client mqtt.Client, s Sensor, topic, deviceID string, mock bool) {
	r, err := s.Read(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("failed to read sensor", "error", err)
		}
		return
	}

	msg := telemetry.Message{
		DeviceID:     deviceID,
		MeasuredAt:   time.Now().UTC(),
		CO2PPM:       r.CO2PPM,
		TemperatureC: round1(r.TemperatureC),
		HumidityPct:  round1(r.HumidityPct),
		AppVersion:   version,
		Mock:         mock,
	}
	slog.Info("measured", "co2_ppm", msg.CO2PPM, "temperature_c", msg.TemperatureC, "humidity_pct", msg.HumidityPct)

	payload, err := json.Marshal(msg)
	if err != nil {
		slog.Error("failed to encode message", "error", err)
		return
	}

	if !client.IsConnectionOpen() {
		slog.Warn("not connected, skipped publish")
		return
	}
	// MQTT に publish する
	token := client.Publish(topic, 1, false, payload)
	if !token.WaitTimeout(10 * time.Second) {
		slog.Error("publish timed out", "topic", topic)
		return
	}
	if err := token.Error(); err != nil {
		slog.Error("publish failed", "topic", topic, "error", err)
		return
	}
	slog.Info("published", "topic", topic)
}

func openSensor(sensorType, i2cDevice string) (Sensor, error) {
	switch sensorType {
	case "scd41":
		return sensor.OpenSCD41(i2cDevice)
	case "mock":
		slog.Warn("using MOCK sensor: values are fake")
		return sensor.NewMock(), nil
	default:
		return nil, fmt.Errorf("unknown SENSOR %q (use scd41 or mock)", sensorType)
	}
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
