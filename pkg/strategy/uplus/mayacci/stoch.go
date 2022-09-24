package mayacci

import (
	"time"

	"github.com/c9s/bbgo/pkg/datatype/floats"
	"github.com/c9s/bbgo/pkg/types"
)

const DPeriod int = 3

var zeroTime = time.Time{}

/*
stoch implements stochastic oscillator indicator

Stochastic Oscillator
- https://www.investopedia.com/terms/s/stochasticoscillator.asp
*/
//go:generate callbackgen -type STOCH
type STOCH struct {
	types.IntervalWindow
	Rsv floats.Slice
	K   floats.Slice
	D   floats.Slice

	HighValues floats.Slice
	LowValues  floats.Slice

	EndTime         time.Time
	UpdateCallbacks []func(k float64, d float64)
}

func (inc *STOCH) Update(high, low, cloze float64) {
	inc.HighValues.Push(high)
	inc.LowValues.Push(low)

	lowest := inc.LowValues.Tail(inc.Window).Min()
	highest := inc.HighValues.Tail(inc.Window).Max()

	if highest == lowest {
		inc.Rsv.Push(50.0)
	} else {
		k := 100.0 * (cloze - lowest) / (highest - lowest)
		inc.Rsv.Push(k)
	}
	k := inc.Rsv.Tail(15).Mean()
	inc.K.Push(k)
	//k = this.sma(rsv, float64(this.n2))
	//d = this.sma(k, float64(this.n3))

	d := inc.K.Tail(DPeriod).Mean()
	inc.D.Push(d)
}

func (inc *STOCH) LastK() float64 {
	if len(inc.K) == 0 {
		return 0.0
	}
	return inc.K[len(inc.K)-1]
}

func (inc *STOCH) LastD() float64 {
	if len(inc.K) == 0 {
		return 0.0
	}
	return inc.D[len(inc.D)-1]
}

func (inc *STOCH) PushK(k types.KLine) {
	if inc.EndTime != zeroTime && !k.EndTime.After(inc.EndTime) {
		return
	}

	inc.Update(k.High.Float64(), k.Low.Float64(), k.Close.Float64())
	inc.EndTime = k.EndTime.Time()
	//inc.EmitUpdate(inc.LastK(), inc.LastD())
}

func (inc *STOCH) GetD() types.Series {
	return &inc.D
}

func (inc *STOCH) GetK() types.Series {
	return &inc.K
}
