package sensor

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// SCD41 の I2C アドレスとコマンド。値は Sensirion の SCD4x データシートと
// 公式ドライバ (github.com/Sensirion/embedded-i2c-scd4x) に合わせている。
const (
	scd41Address = 0x62

	cmdStartPeriodicMeasurement = 0x21b1
	cmdStopPeriodicMeasurement  = 0x3f86
	cmdGetDataReadyStatus       = 0xe4b8
	cmdReadMeasurement          = 0xec05

	// Linux の i2c-dev で「この fd はどのアドレスと話すか」を設定する ioctl 番号。
	i2cSlave = 0x0703
)

type Reading struct {
	CO2PPM       int
	TemperatureC float64
	HumidityPct  float64
}

// SCD41 は Linux の /dev/i2c-N を通して SCD41 と直接やりとりする。
// i2c-dev では I2C バスを普通のファイルとして read/write できる。
type SCD41 struct {
	f *os.File
}

// OpenSCD41 はセンサーを開き、5 秒周期の定期測定を開始する。
func OpenSCD41(device string) (*SCD41, error) {
	f, err := os.OpenFile(device, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", device, err)
	}
	if err := unix.IoctlSetInt(int(f.Fd()), i2cSlave, scd41Address); err != nil {
		f.Close()
		return nil, fmt.Errorf("set i2c address: %w", err)
	}
	s := &SCD41{f: f}

	// 前のプロセスが測定を止めずに終了していると start が受け付けられないため、先に止める。
	if err := s.command(cmdStopPeriodicMeasurement); err != nil {
		f.Close()
		return nil, fmt.Errorf("stop periodic measurement: %w", err)
	}
	time.Sleep(500 * time.Millisecond)

	if err := s.command(cmdStartPeriodicMeasurement); err != nil {
		f.Close()
		return nil, fmt.Errorf("start periodic measurement: %w", err)
	}
	return s, nil
}

// Read は次の測定値が出るまで待ってから読み取る。SCD41 は約 5 秒ごとに値を更新する。
func (s *SCD41) Read(ctx context.Context) (Reading, error) {
	for {
		ready, err := s.dataReady()
		if err != nil {
			return Reading{}, err
		}
		if ready {
			break
		}
		select {
		case <-ctx.Done():
			return Reading{}, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	words, err := s.readWords(cmdReadMeasurement, 3)
	if err != nil {
		return Reading{}, fmt.Errorf("read measurement: %w", err)
	}
	return decodeMeasurement(words), nil
}

// Close は測定を止めてからファイルを閉じる。
func (s *SCD41) Close() error {
	stopErr := s.command(cmdStopPeriodicMeasurement)
	time.Sleep(500 * time.Millisecond)
	closeErr := s.f.Close()
	if stopErr != nil {
		return stopErr
	}
	return closeErr
}

func (s *SCD41) dataReady() (bool, error) {
	words, err := s.readWords(cmdGetDataReadyStatus, 1)
	if err != nil {
		return false, fmt.Errorf("get data ready status: %w", err)
	}
	// 下位 11 ビットのどれかが立っていれば新しい測定値がある。
	return words[0]&0x07ff != 0, nil
}

func (s *SCD41) command(cmd uint16) error {
	_, err := s.f.Write([]byte{byte(cmd >> 8), byte(cmd)})
	return err
}

// readWords はコマンドを送り、n 個の 16 ビット値を読む。
// センサーは 1 ワードごとに「上位バイト・下位バイト・CRC」の 3 バイトを返す。
func (s *SCD41) readWords(cmd uint16, n int) ([]uint16, error) {
	if err := s.command(cmd); err != nil {
		return nil, err
	}
	time.Sleep(time.Millisecond)

	buf := make([]byte, n*3)
	if _, err := s.f.Read(buf); err != nil {
		return nil, err
	}
	return parseWords(buf)
}

func parseWords(buf []byte) ([]uint16, error) {
	words := make([]uint16, 0, len(buf)/3)
	for i := 0; i+2 < len(buf); i += 3 {
		if got, want := buf[i+2], crc8(buf[i:i+2]); got != want {
			return nil, fmt.Errorf("crc mismatch at word %d: got 0x%02x want 0x%02x", i/3, got, want)
		}
		words = append(words, uint16(buf[i])<<8|uint16(buf[i+1]))
	}
	return words, nil
}

// decodeMeasurement はデータシートの換算式で生の値を単位付きの値にする。
func decodeMeasurement(words []uint16) Reading {
	return Reading{
		CO2PPM:       int(words[0]),
		TemperatureC: -45 + 175*float64(words[1])/65535,
		HumidityPct:  100 * float64(words[2]) / 65535,
	}
}

// crc8 は Sensirion 仕様の CRC-8（多項式 0x31、初期値 0xFF）。
func crc8(data []byte) byte {
	crc := byte(0xff)
	for _, b := range data {
		crc ^= b
		for range 8 {
			if crc&0x80 != 0 {
				crc = crc<<1 ^ 0x31
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
