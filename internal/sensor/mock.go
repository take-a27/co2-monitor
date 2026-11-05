package sensor

import (
	"context"
	"math/rand/v2"
)

// Mock はそれらしい値を少しずつ揺らして返す。
type Mock struct {
	last Reading
}

func NewMock() *Mock {
	return &Mock{last: Reading{CO2PPM: 600, TemperatureC: 24.0, HumidityPct: 45.0}}
}

func (m *Mock) Read(context.Context) (Reading, error) {
	m.last.CO2PPM = clamp(m.last.CO2PPM+rand.IntN(41)-20, 400, 2000)
	m.last.TemperatureC += (rand.Float64() - 0.5) * 0.2
	m.last.HumidityPct += (rand.Float64() - 0.5) * 0.5
	return m.last, nil
}

func (m *Mock) Close() error { return nil }

func clamp(v, lo, hi int) int {
	return max(lo, min(v, hi))
}
