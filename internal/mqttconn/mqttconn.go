// Package mqttconn は MQTT ブローカーへの接続をまとめる。
// ローカルの mosquitto（平文）と AWS IoT Core（証明書による TLS 相互認証）の両方に使う。
package mqttconn

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Config struct {
	// 例: tcp://localhost:1883 / ssl://xxxx-ats.iot.ap-northeast-1.amazonaws.com:8883
	Broker   string `env:"MQTT_BROKER" envDefault:"tcp://localhost:1883"`
	ClientID string `env:"MQTT_CLIENT_ID"`
	CAFile   string `env:"MQTT_CA_FILE"`
	CertFile string `env:"MQTT_CERT_FILE"`
	KeyFile  string `env:"MQTT_KEY_FILE"`
}

// Connect は接続を開始する。ブローカーに届かなくても裏で再試行を続けるので、
// 起動時にネットワークがまだ無くてもプロセスは落ちない。 onConnect は接続（再接続を含む）のたびに呼ばれる。
func Connect(cfg Config, onConnect func(mqtt.Client)) (mqtt.Client, error) {
	opts := mqtt.NewClientOptions().
		AddBroker(cfg.Broker).
		SetClientID(cfg.ClientID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetOnConnectHandler(func(c mqtt.Client) {
			slog.Info("mqtt connected", "broker", cfg.Broker, "client_id", cfg.ClientID)
			if onConnect != nil {
				onConnect(c)
			}
		}).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			slog.Warn("mqtt connection lost", "error", err)
		}).
		SetReconnectingHandler(func(mqtt.Client, *mqtt.ClientOptions) {
			slog.Info("mqtt reconnecting")
		})

	if cfg.CertFile != "" {
		tlsConfig, err := newTLSConfig(cfg)
		if err != nil {
			return nil, err
		}
		opts.SetTLSConfig(tlsConfig)
	}

	client := mqtt.NewClient(opts)
	client.Connect()
	return client, nil
}

func newTLSConfig(cfg Config) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load client certificate: %w", err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("no certificates found in %s", cfg.CAFile)
		}
		tlsConfig.RootCAs = pool
	}
	return tlsConfig, nil
}
