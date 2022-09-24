package dmiema

import "github.com/c9s/bbgo/pkg/indicator"

type Rmca struct {
	alma *indicator.ALMA
	rsi  *indicator.RSI
	cci  *indicator.CCI
	macd *indicator.MACD

	maSlow  *indicator.EWMA
	maQuick *indicator.EWMA
}

func (s *Rmca) Index(i int) float64 {
	return s.maQuick.Index(i)/s.maSlow.Index(i) - 1.0
}

func (s *Rmca) Last() float64 {
	return s.maQuick.Last()/s.maSlow.Last() - 1.0
}

func (s *Rmca) Length() int {
	return s.maSlow.Length()
}

func (s *Rmca) Update(v float64) {
	s.maSlow.Update(v)
	s.maQuick.Update(v)
}
