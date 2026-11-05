// Package telemetry はデバイスとサーバーで共有する MQTT メッセージの形式を定義する。
package telemetry

import "time"

// SubscribeTopic は全デバイスのテレメトリを購読するためのトピックフィルタ。
const SubscribeTopic = "devices/+/telemetry"

type Message struct {
	DeviceID     string    `json:"device_id"`
	MeasuredAt   time.Time `json:"measured_at"`
	CO2PPM       int       `json:"co2_ppm"`
	TemperatureC float64   `json:"temperature_c"`
	HumidityPct  float64   `json:"humidity_pct"`
	AppVersion   string    `json:"app_version"`
	Mock         bool      `json:"mock"`
}

func Topic(deviceID string) string {
	return "devices/" + deviceID + "/telemetry"
}
