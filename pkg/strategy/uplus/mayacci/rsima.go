package mayacci

import (
	"github.com/c9s/bbgo/pkg/indicator"
	"github.com/c9s/bbgo/pkg/types"
)

type RsiMA struct {
	types.SeriesBase
	rsi   *indicator.RSI
	rsiMa types.UpdatableSeriesExtend
	ma2   types.UpdatableSeriesExtend
}

func (s *RsiMA) Update(value float64) {

	s.rsi.Update(value)
	s.rsiMa.Update(s.rsi.Last())
}

func (s *RsiMA) Last() (float64, float64) {
	return s.rsi.Last(), s.rsiMa.Last()
}

func (s *RsiMA) Index(i int) (float64, float64) {
	return s.rsi.Index(i), s.rsiMa.Index(i)
}

func (s *RsiMA) Length() int {
	return s.rsi.Length()
}

func (s *RsiMA) Cross() types.SideType {
	var side types.SideType

	crossOver := types.CrossOver(s.rsi, s.rsiMa)
	crossUnder := types.CrossUnder(s.rsi, s.rsiMa)

	if crossOver.Last() && !crossOver.Index(1) {
		side = types.SideTypeBuy
	}
	if crossUnder.Last() && !crossUnder.Index(1) {
		side = types.SideTypeSell
	}
	return side

}
