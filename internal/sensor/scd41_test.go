package sensor

import (
	"math"
	"testing"
)

// テストの値は SCD4x データシート (Version 1.5, July 2023) の例そのもの。

func TestCRC8(t *testing.T) {
	// 3.11 Checksum Calculation: CRC(0xbeef) = 0x92
	if got := crc8([]byte{0xbe, 0xef}); got != 0x92 {
		t.Fatalf("crc8(0xBEEF) = 0x%02x, want 0x92", got)
	}
}

func TestParseWordsAndDecode(t *testing.T) {
	// Table 11 read_measurement の応答例: 500 ppm, 25 °C, 37 %RH。
	// ただし 0x01f4 の CRC はデータシートでは 0x7b と書かれているが、
	// 3.11 の計算方法では 0x33 になる（ほかの例 11 個は一致する）ので、0x33 を使う。
	raw := []byte{0x01, 0xf4, 0x33, 0x66, 0x67, 0xa2, 0x5e, 0xb9, 0x3c}
	words, err := parseWords(raw)
	if err != nil {
		t.Fatal(err)
	}
	r := decodeMeasurement(words)

	if r.CO2PPM != 500 {
		t.Errorf("CO2 = %d, want 500", r.CO2PPM)
	}
	if math.Abs(r.TemperatureC-25.0) > 0.01 {
		t.Errorf("temperature = %.3f, want 25.0", r.TemperatureC)
	}
	if math.Abs(r.HumidityPct-37.0) > 0.01 {
		t.Errorf("humidity = %.3f, want 37.0", r.HumidityPct)
	}
}

func TestDataReadyNotReady(t *testing.T) {
	// Table 23 get_data_ready_status の応答例: 0x8000 は下位 11 ビットが 0 → まだ準備できていない
	words, err := parseWords([]byte{0x80, 0x00, 0xa2})
	if err != nil {
		t.Fatal(err)
	}
	if words[0]&0x07ff != 0 {
		t.Fatalf("0x%04x should mean not ready", words[0])
	}
}

func TestParseWordsRejectsBadCRC(t *testing.T) {
	if _, err := parseWords([]byte{0xbe, 0xef, 0x00}); err == nil {
		t.Fatal("expected crc error")
	}
}
