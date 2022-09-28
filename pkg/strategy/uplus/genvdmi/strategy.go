package genvdmi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/c9s/bbgo/pkg/strategy/uplus/indi"
	"math"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/wcharczuk/go-chart/v2"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/datatype/floats"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/indicator"
	"github.com/c9s/bbgo/pkg/interact"
	"github.com/c9s/bbgo/pkg/types"
	"github.com/c9s/bbgo/pkg/util"
)

const ID = "genvdmi"

var log = logrus.WithField("strategy", ID)
var Four fixedpoint.Value = fixedpoint.NewFromInt(4)
var Three fixedpoint.Value = fixedpoint.NewFromInt(3)
var Two fixedpoint.Value = fixedpoint.NewFromInt(2)
var Delta fixedpoint.Value = fixedpoint.NewFromFloat(0.01)
var Fee = 0.0008 // taker fee % * 2, for upper bound
var BUY = "1"
var SELL = "-1"
var HOLD = "0"
var holdingMax = 5

func init() {
	bbgo.RegisterStrategy(ID, &Strategy{})
}

type Strategy struct {
	Symbol string `json:"symbol"`

	bbgo.OpenPositionOptions
	bbgo.StrategyController
	types.Market
	types.IntervalWindow
	bbgo.SourceSelector

	*bbgo.Environment
	*types.Position    `persistence:"position"`
	*types.ProfitStats `persistence:"profit_stats"`
	*types.TradeStats  `persistence:"trade_stats"`

	p *types.Position

	priceLines *types.Queue
	trendLine  types.UpdatableSeriesExtend
	ma         types.UpdatableSeriesExtend
	stdevHigh  *indicator.StdDev
	stdevLow   *indicator.StdDev

	atr                 *indicator.ATR
	midPrice            fixedpoint.Value
	lock                sync.RWMutex `ignore:"true"`
	positionLock        sync.RWMutex `ignore:"true"`
	startTime           time.Time
	minutesCounter      int
	orderPendingCounter map[uint64]int
	frameKLine          *types.KLine
	kline1m             *types.KLine

	beta float64

	StopLoss                  fixedpoint.Value `json:"stoploss" modifiable:"true"`
	CanvasPath                string           `json:"canvasPath"`
	PredictOffset             int              `json:"predictOffset"`
	HighLowVarianceMultiplier float64          `json:"hlVarianceMultiplier" modifiable:"true"`
	NoTrailingStopLoss        bool             `json:"noTrailingStopLoss" modifiable:"true"`
	TrailingStopLossType      string           `json:"trailingStopLossType" modifiable:"true"` // trailing stop sources. Possible options are `kline` for 1m kline and `realtime` from order updates
	HLRangeWindow             int              `json:"hlRangeWindow"`
	Window1m                  int              `json:"window1m"`
	FisherTransformWindow1m   int              `json:"fisherTransformWindow1m"`
	SmootherWindow1m          int              `json:"smootherWindow1m"`
	SmootherWindow            int              `json:"smootherWindow"`
	FisherTransformWindow     int              `json:"fisherTransformWindow"`
	ATRWindow                 int              `json:"atrWindow"`
	PendingMinutes            int              `json:"pendingMinutes" modifiable:"true"`  // if order not be traded for pendingMinutes of time, cancel it.
	NoRebalance               bool             `json:"noRebalance" modifiable:"true"`     // disable rebalance
	TrendWindow               int              `json:"trendWindow"`                       // trendLine is used for rebalancing the position. When trendLine goes up, hold base, otherwise hold quote
	RebalanceFilter           float64          `json:"rebalanceFilter" modifiable:"true"` // beta filter on the Linear Regression of trendLine
	TrailingCallbackRate      []float64        `json:"trailingCallbackRate" modifiable:"true"`
	TrailingActivationRatio   []float64        `json:"trailingActivationRatio" modifiable:"true"`

	DriftFilterNeg  float64 //`json:"driftFilterNeg" modifiable:"true"`
	DriftFilterPos  float64 //`json:"driftFilterPos" modifiable:"true"`
	DDriftFilterNeg float64 //`json:"ddriftFilterNeg" modifiable:"true"`
	DDriftFilterPos float64 //`json:"ddriftFilterPos" modifiable:"true"`

	buyPrice     float64 `persistence:"buy_price"`
	sellPrice    float64 `persistence:"sell_price"`
	highestPrice float64 `persistence:"highest_price"`
	lowestPrice  float64 `persistence:"lowest_price"`

	// This is not related to trade but for statistics graph generation
	// Will deduct fee in percentage from every trade
	GraphPNLDeductFee bool   `json:"graphPNLDeductFee"`
	GraphPNLPath      string `json:"graphPNLPath"`
	GraphCumPNLPath   string `json:"graphCumPNLPath"`
	// Whether to generate graph when shutdown
	GenerateGraph bool `json:"generateGraph"`

	ExitMethods bbgo.ExitMethodSet `json:"exits"`
	Session     *bbgo.ExchangeSession
	*bbgo.GeneralOrderExecutor

	getLastPrice func() fixedpoint.Value

	WindowATR   int              `json:"windowATR"`
	WindowATR2  int              `json:"windowATR2"`
	WindowRSI   int              `json:"windowRSI"`
	WindowCCI   int              `json:"windowCCI"`
	WindowHMA   int              `json:"windowHMA"`
	WindowALMA  int              `json:"windowALMA"`
	WindowMACD  int              `json:"windowMACD"`
	WindowSTOCH int              `json:"windowSTOCH"`
	WindowDmi   int              `json:"windowDMI"`
	WindowQuick int              `json:"windowQuick"`
	WindowSlow  fixedpoint.Value `json:"windowSlow,omitempty"`
	WindowDEMA  fixedpoint.Value `json:"windowDEMA,omitempty"`
	WindowST    int              `json:"windowSt"`
	MaType      string           `json:"maType,omitempty"`
	//ewo        *ElliottWave
	emaFast *indicator.EWMA
	emaSlow *indicator.EWMA
	alma    *indicator.ALMA
	dema    *indicator.DEMA
	sma     *indicator.SMA

	// SuperTrend indicator
	Supertrend *indicator.Supertrend
	// SupertrendMultiplier ATR multiplier for calculation of supertrend
	SupertrendMultiplier float64 `json:"supertrendMultiplier"`

	macd *DEMACD

	rsi            *indicator.RSI
	cci            *indicator.CCI
	dmi            *indicator.DMI
	hma            *indi.HMA
	hull           *indicator.HULL
	change         *indi.Slice
	holdingCounter int

	atr2  *indicator.ATR
	stoch *indicator.STOCH

	currentTakeProfitPrice fixedpoint.Value
	currentStopLossPrice   fixedpoint.Value

	// TakeProfitAtrMultiplier TP according to ATR multiple, 0 to disable this
	TakeProfitAtrMultiplier float64 `json:"takeProfitAtrMultiplier"`
}

func (s *Strategy) ID() string {
	return ID
}

func (s *Strategy) InstanceID() string {
	return fmt.Sprintf("%s:%s:%v", ID, s.Symbol, bbgo.IsBackTesting)
}

func (s *Strategy) Subscribe(session *bbgo.ExchangeSession) {

	session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{
		Interval: types.Interval1m,
	})
	session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{Interval: s.Interval})

	if !bbgo.IsBackTesting {
		session.Subscribe(types.BookTickerChannel, s.Symbol, types.SubscribeOptions{})
	}
	s.ExitMethods.SetAndSubscribe(session, s)
}

func (s *Strategy) CurrentPosition() *types.Position {
	return s.Position
}

func (s *Strategy) ClosePosition(ctx context.Context, percentage fixedpoint.Value) bool {
	order := s.p.NewMarketCloseOrder(percentage)
	if order == nil {
		s.positionLock.Unlock()
		return false
	}
	order.Tag = "close"
	order.TimeInForce = ""
	balances := s.GeneralOrderExecutor.Session().GetAccount().Balances()
	baseBalance := balances[s.Market.BaseCurrency].Available
	price := s.getLastPrice()
	if order.Side == types.SideTypeBuy {
		quoteAmount := balances[s.Market.QuoteCurrency].Available.Div(price)
		if order.Quantity.Compare(quoteAmount) > 0 {
			order.Quantity = quoteAmount
		}
	} else if order.Side == types.SideTypeSell && order.Quantity.Compare(baseBalance) > 0 {
		order.Quantity = baseBalance
	}
	order.MarginSideEffect = types.SideEffectTypeAutoRepay
	s.positionLock.Unlock()
	for {
		if s.Market.IsDustQuantity(order.Quantity, price) {
			return false
		}
		_, err := s.GeneralOrderExecutor.SubmitOrders(ctx, *order)
		if err != nil {
			order.Quantity = order.Quantity.Mul(fixedpoint.One.Sub(Delta))
			continue
		}
		return true
	}
}

func (s *Strategy) initIndicators(store *bbgo.MarketDataStore) error {
	s.change = &indi.Slice{}
	s.ma = &indicator.SMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.HLRangeWindow}}
	s.stdevHigh = &indicator.StdDev{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.HLRangeWindow}}
	s.stdevLow = &indicator.StdDev{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.HLRangeWindow}}
	s.trendLine = &indicator.EWMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.TrendWindow}}

	s.emaFast = &indicator.EWMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSlow.Int()}}
	s.emaSlow = &indicator.EWMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowQuick}}
	s.dmi = &indicator.DMI{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowDmi}, ADXSmoothing: s.WindowDmi}
	s.trendLine = &indicator.EWMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.TrendWindow}}

	s.stoch = &indicator.STOCH{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSTOCH}}

	s.atr = &indicator.ATR{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowATR}}
	s.atr2 = &indicator.ATR{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowATR2}}
	s.rsi = &indicator.RSI{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowRSI}}
	s.cci = &indicator.CCI{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowCCI}}
	s.alma = &indicator.ALMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSlow.Int()}, Offset: 0.775, Sigma: 5}
	s.dema = &indicator.DEMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSlow.Int()}}
	s.sma = &indicator.SMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSlow.Int()}}
	s.hull = &indicator.HULL{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSlow.Int()}}

	s.macd = &DEMACD{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowMACD}, ShortPeriod: 12, LongPeriod: 26, DeaPeriod: 9, MaType: "EWMA"}

	s.hma = &indi.HMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowHMA}}

	if s.SupertrendMultiplier == 0 {
		s.SupertrendMultiplier = 3
	}
	s.Supertrend = &indicator.Supertrend{IntervalWindow: types.IntervalWindow{Window: s.WindowST, Interval: s.Interval}, ATRMultiplier: s.SupertrendMultiplier}
	s.Supertrend.AverageTrueRange = &indicator.ATR{IntervalWindow: types.IntervalWindow{Window: s.WindowST, Interval: s.Interval}}
	s.Supertrend.BindK(s.Session.MarketDataStream, s.Symbol, s.Supertrend.Interval)

	klines, ok := store.KLinesOfInterval(s.Interval)
	klineLength := len(*klines)
	if !ok || klineLength == 0 {
		return errors.New("klines not exists")
	}
	if s.frameKLine != nil && klines != nil {
		s.frameKLine.Set(&(*klines)[len(*klines)-1])
	}
	for _, kline := range *klines {
		s.AddKline(kline)

	}

	//s.Supertrend.LoadK((*klines)[0:])
	return nil
}

func (s *Strategy) AddKline(kline types.KLine) {
	source := s.GetSource(&kline).Float64()
	high := kline.High.Float64()
	low := kline.Low.Float64()
	s.ma.Update(source)
	s.stdevHigh.Update(high - s.ma.Last())
	s.stdevLow.Update(s.ma.Last() - low)

	s.atr.PushK(kline)
	s.atr2.PushK(kline)

	s.rsi.Update(kline.Volume.Float64())
	s.hma.Update(s.rsi.Last())

	s.cci.PushK(kline)

	s.alma.Update(source)
	s.emaFast.PushK(kline)
	s.emaSlow.PushK(kline)
	s.dema.PushK(kline)
	s.sma.PushK(kline)
	s.hull.PushK(kline)

	s.stoch.PushK(kline)

	s.dmi.PushK(kline)
	s.macd.PushK(kline)
	s.Supertrend.PushK(kline)

	s.trendLine.Update(source)

	s.priceLines.Update(source)

	//fmt.Println(kline)
	//stSignal := s.Supertrend.GetSignal()

	//fmt.Printf("macd %.4f dif：%.4f,dea %.4f   \n", s.macd.Last(), s.macd.Dif.Last(), s.macd.Dea.Last())
	//fmt.Printf("stsignal %d spv：%.4f,spatr %.4f   \n", stSignal, s.Supertrend.Last(), s.Supertrend.AverageTrueRange.Last())

	//fmt.Printf("rsi %.4f cci：%.4f,atr %.4f  \n", s.rsi.Last(), s.cci.Last(), s.atr.Last())
	//fmt.Printf("rsi %.4f cci：%.4f,atr %.4f  \n", s.rsi.Last(), s.cci.Last(), s.atr.Last())
	//fmt.Printf("time%s,close%.4f,dip:%.4f,dim:%.4f,adx:%.4f\n", kline.EndTime, kline.Close.Float64(), s.dmi.GetDIPlus().Last(), s.dmi.GetDIMinus().Last(), s.dmi.GetADX().Last())
	//fmt.Printf("ema:%.4f,hma:%.4f \n", s.emaSlow.Last(), s.hma.Last())
	//fmt.Printf("hma:%.4f,rsi:%.4f \n", s.hma.Last(), s.rsi.Last())
	//fmt.Printf("atr1:%.4f,atr2:%.4f  \n", s.atr.Last(), s.atr2.Last())
}
func (s *Strategy) UpdateKline(kline types.KLine) {
	source := s.GetSource(&kline).Float64()

	s.ma.Update(source)

	s.atr.PushK(kline)
	s.atr2.PushK(kline)

	s.rsi.Update(kline.Volume.Float64())
	s.hma.Update(s.rsi.Last())

	s.cci.PushK(kline)

	s.alma.Update(source)
	s.emaFast.PushK(kline)
	s.emaSlow.PushK(kline)
	s.dema.PushK(kline)
	s.sma.PushK(kline)
	s.hull.PushK(kline)

	s.stoch.PushK(kline)

	s.dmi.PushK(kline)
	s.macd.PushK(kline)
	s.Supertrend.PushK(kline)

	s.trendLine.Update(source)

	s.priceLines.Update(source)
	s.frameKLine.Set(&kline)
	//fmt.Println(kline)
	//stSignal := s.Supertrend.GetSignal()

	//fmt.Printf("macd %.4f dif：%.4f,dea %.4f   \n", s.macd.Last(), s.macd.Dif.Last(), s.macd.Dea.Last())
	//fmt.Printf("stsignal %d spv：%.4f,spatr %.4f   \n", stSignal, s.Supertrend.Last(), s.Supertrend.AverageTrueRange.Last())

	//fmt.Printf("rsi %.4f cci：%.4f,atr %.4f  \n", s.rsi.Last(), s.cci.Last(), s.atr.Last())
	//fmt.Printf("rsi %.4f cci：%.4f,atr %.4f  \n", s.rsi.Last(), s.cci.Last(), s.atr.Last())
	//fmt.Printf("time%s,close%.4f,dip:%.4f,dim:%.4f,adx:%.4f\n", kline.EndTime, kline.Close.Float64(), s.dmi.GetDIPlus().Last(), s.dmi.GetDIMinus().Last(), s.dmi.GetADX().Last())
	//fmt.Printf("ema:%.4f,hma:%.4f \n", s.emaSlow.Last(), s.hma.Last())
	//fmt.Printf("hma:%.4f,rsi:%.4f \n", s.hma.Last(), s.rsi.Last())
	//fmt.Printf("atr1:%.4f,atr2:%.4f  \n", s.atr.Last(), s.atr2.Last())
}

func (s *Strategy) smartCancel(ctx context.Context, pricef, atr float64) (int, error) {
	nonTraded := s.GeneralOrderExecutor.ActiveMakerOrders().Orders()
	if len(nonTraded) > 0 {
		if len(nonTraded) > 1 {
			log.Errorf("should only have one order to cancel, got %d", len(nonTraded))
		}
		toCancel := false

		for _, order := range nonTraded {
			if order.Status != types.OrderStatusNew && order.Status != types.OrderStatusPartiallyFilled {
				continue
			}
			log.Warnf("%v | counter: %d, system: %d", order, s.orderPendingCounter[order.OrderID], s.minutesCounter)
			if order.Side == types.SideTypeBuy {
				// 75% of the probability
				if order.Price.Float64()+s.stdevHigh.Last()*2 <= pricef {
					toCancel = true
				}
			} else if order.Side == types.SideTypeSell {
				// 75% of the probability
				if order.Price.Float64()-s.stdevLow.Last()*2 >= pricef {
					toCancel = true
				}
			} else {
				panic("not supported side for the order")
			}
		}
		if toCancel {
			err := s.GeneralOrderExecutor.GracefulCancel(ctx)
			// TODO: clean orderPendingCounter on cancel/trade
			if err == nil {
				for _, order := range nonTraded {
					delete(s.orderPendingCounter, order.OrderID)
				}
			}
			log.Warnf("cancel all %v", err)
			return 0, err
		}
	}
	return len(nonTraded), nil
}

func (s *Strategy) trailingCheck(price float64, direction string) bool {
	if s.highestPrice > 0 && s.highestPrice < price {
		s.highestPrice = price
	}
	if s.lowestPrice > 0 && s.lowestPrice > price {
		s.lowestPrice = price
	}
	isShort := direction == "short"
	for i := len(s.TrailingCallbackRate) - 1; i >= 0; i-- {
		trailingCallbackRate := s.TrailingCallbackRate[i]
		trailingActivationRatio := s.TrailingActivationRatio[i]
		if isShort {
			if (s.sellPrice-s.lowestPrice)/s.lowestPrice > trailingActivationRatio {
				return (price-s.lowestPrice)/s.lowestPrice > trailingCallbackRate
			}
		} else {
			if (s.highestPrice-s.buyPrice)/s.buyPrice > trailingActivationRatio {
				return (s.highestPrice-price)/price > trailingCallbackRate
			}
		}
	}
	return false
}

func (s *Strategy) initTickerFunctions(ctx context.Context) {
	if s.IsBackTesting() {
		s.getLastPrice = func() fixedpoint.Value {
			lastPrice, ok := s.Session.LastPrice(s.Symbol)
			if !ok {
				log.Error("cannot get lastprice")
			}
			return lastPrice
		}
	} else {
		s.Session.MarketDataStream.OnBookTickerUpdate(func(ticker types.BookTicker) {
			bestBid := ticker.Buy
			bestAsk := ticker.Sell

			var pricef float64
			if !util.TryLock(&s.lock) {
				return
			}
			if !bestAsk.IsZero() && !bestBid.IsZero() {
				s.midPrice = bestAsk.Add(bestBid).Div(Two)
			} else if !bestAsk.IsZero() {
				s.midPrice = bestAsk
			} else {
				s.midPrice = bestBid
			}
			pricef = s.midPrice.Float64()

			s.lock.Unlock()

			if !util.TryLock(&s.positionLock) {
				return
			}

			if s.highestPrice > 0 && s.highestPrice < pricef {
				s.highestPrice = pricef
			}
			if s.lowestPrice > 0 && s.lowestPrice > pricef {
				s.lowestPrice = pricef
			}
			// for trailing stoploss during the realtime
			if s.NoTrailingStopLoss || s.TrailingStopLossType == "kline" {
				s.positionLock.Unlock()
				return
			}

			stoploss := s.StopLoss.Float64()

			exitShortCondition := s.sellPrice > 0 && (s.sellPrice*(1.+stoploss) <= pricef ||
				s.trailingCheck(pricef, "short"))
			exitLongCondition := s.buyPrice > 0 && (s.buyPrice*(1.-stoploss) >= pricef ||
				s.trailingCheck(pricef, "long"))
			if exitShortCondition || exitLongCondition {
				if s.ClosePosition(ctx, fixedpoint.One) {
					log.Infof("close position by orderbook changes")
				}
			} else {
				s.positionLock.Unlock()
			}
		})
		s.getLastPrice = func() (lastPrice fixedpoint.Value) {
			var ok bool
			s.lock.RLock()
			defer s.lock.RUnlock()
			if s.midPrice.IsZero() {
				lastPrice, ok = s.Session.LastPrice(s.Symbol)
				if !ok {
					log.Error("cannot get lastprice")
					return lastPrice
				}
			} else {
				lastPrice = s.midPrice
			}
			return lastPrice
		}
	}

}

// Sending new rebalance orders cost too much.
// Modify the position instead to expect the strategy itself rebalance on Close
func (s *Strategy) Rebalance(ctx context.Context) {
	price := s.getLastPrice()
	_, beta := types.LinearRegression(s.trendLine, 3)
	if math.Abs(beta) > s.RebalanceFilter && math.Abs(s.beta) > s.RebalanceFilter || math.Abs(s.beta) < s.RebalanceFilter && math.Abs(beta) < s.RebalanceFilter {
		return
	}
	balances := s.GeneralOrderExecutor.Session().GetAccount().Balances()
	baseBalance := balances[s.Market.BaseCurrency].Total()
	quoteBalance := balances[s.Market.QuoteCurrency].Total()
	total := baseBalance.Add(quoteBalance.Div(price))
	percentage := fixedpoint.One.Sub(Delta)
	log.Infof("rebalance beta %f %v", beta, s.p)
	if beta > s.RebalanceFilter {
		if total.Mul(percentage).Compare(baseBalance) > 0 {
			q := total.Mul(percentage).Sub(baseBalance)
			s.p.Lock()
			defer s.p.Unlock()
			s.p.Base = q.Neg()
			s.p.Quote = q.Mul(price)
			s.p.AverageCost = price
		}
	} else if beta <= -s.RebalanceFilter {
		if total.Mul(percentage).Compare(quoteBalance.Div(price)) > 0 {
			q := total.Mul(percentage).Sub(quoteBalance.Div(price))
			s.p.Lock()
			defer s.p.Unlock()
			s.p.Base = q
			s.p.Quote = q.Mul(price).Neg()
			s.p.AverageCost = price
		}
	} else {
		if total.Div(Two).Compare(quoteBalance.Div(price)) > 0 {
			q := total.Div(Two).Sub(quoteBalance.Div(price))
			s.p.Lock()
			defer s.p.Unlock()
			s.p.Base = q
			s.p.Quote = q.Mul(price).Neg()
			s.p.AverageCost = price
		} else if total.Div(Two).Compare(baseBalance) > 0 {
			q := total.Div(Two).Sub(baseBalance)
			s.p.Lock()
			defer s.p.Unlock()
			s.p.Base = q.Neg()
			s.p.Quote = q.Mul(price)
			s.p.AverageCost = price
		} else {
			s.p.Lock()
			defer s.p.Unlock()
			s.p.Reset()
		}
	}
	log.Infof("rebalanceafter %v %v %v", baseBalance, quoteBalance, s.p)
	s.beta = beta
}

func (s *Strategy) CalcAssetValue(price fixedpoint.Value) fixedpoint.Value {
	balances := s.Session.GetAccount().Balances()
	return balances[s.Market.BaseCurrency].Total().Mul(price).Add(balances[s.Market.QuoteCurrency].Total())
}

func (s *Strategy) klineHandler1m(ctx context.Context, kline types.KLine) {
	s.kline1m.Set(&kline)
	if s.Status != types.StrategyStatusRunning {
		return
	}
	// for doing the trailing stoploss during backtesting
	atr := s.atr.Last()
	price := s.getLastPrice()
	pricef := price.Float64()
	stoploss := s.StopLoss.Float64()

	lowf := math.Min(kline.Low.Float64(), pricef)
	highf := math.Max(kline.High.Float64(), pricef)
	s.positionLock.Lock()
	if s.lowestPrice > 0 && lowf < s.lowestPrice {
		s.lowestPrice = lowf
	}
	if s.highestPrice > 0 && highf > s.highestPrice {
		s.highestPrice = highf
	}

	numPending := 0
	var err error
	if numPending, err = s.smartCancel(ctx, pricef, atr); err != nil {
		log.WithError(err).Errorf("cannot cancel orders")
		s.positionLock.Unlock()
		return
	}
	if numPending > 0 {
		s.positionLock.Unlock()
		return
	}

	if s.NoTrailingStopLoss || s.TrailingStopLossType == "realtime" {
		s.positionLock.Unlock()
		return
	}

	//log.Infof("d1m: %f, hf: %f, lf: %f", s.drift1m.Last(), highf, lowf)
	exitShortCondition := s.sellPrice > 0 && (s.sellPrice*(1.+stoploss) <= highf ||
		s.trailingCheck(highf, "short") /* || s.drift1m.Last() > 0*/)
	exitLongCondition := s.buyPrice > 0 && (s.buyPrice*(1.-stoploss) >= lowf ||
		s.trailingCheck(lowf, "long") /* || s.drift1m.Last() < 0*/)
	if exitShortCondition || exitLongCondition {
		_ = s.ClosePosition(ctx, fixedpoint.One)
	} else {
		s.positionLock.Unlock()
	}
}

func (s *Strategy) klineHandler(ctx context.Context, kline types.KLine) {
	source := s.GetSource(&kline)
	sourcef := source.Float64()
	price := s.getLastPrice()
	pricef := price.Float64()
	lowf := math.Min(kline.Low.Float64(), pricef)
	highf := math.Max(kline.High.Float64(), pricef)
	lowdiff := s.ma.Last() - lowf
	s.stdevLow.Update(lowdiff)
	highdiff := highf - s.ma.Last()
	s.stdevHigh.Update(highdiff)

	if s.Status != types.StrategyStatusRunning {
		return
	}
	stoploss := s.StopLoss.Float64()

	s.positionLock.Lock()
	fmt.Println("nimma :", s.positionLock)

	log.Infof("highdiff: %3.2f ma: %.2f, open: %8v, close: %8v, high: %8v, low: %8v, time: %v %v", s.stdevHigh.Last(), s.ma.Last(), kline.Open, kline.Close, kline.High, kline.Low, kline.StartTime, kline.EndTime)
	if s.lowestPrice > 0 && lowf < s.lowestPrice {
		s.lowestPrice = lowf
	}
	if s.highestPrice > 0 && highf > s.highestPrice {
		s.highestPrice = highf
	}

	if !s.NoRebalance {
		s.Rebalance(ctx)
	}

	balances := s.GeneralOrderExecutor.Session().GetAccount().Balances()
	bbgo.Notify("source: %.4f, price: %.4f,  atr: %.4f, lowf %.4f, highf: %.4f lowest: %.4f highest: %.4f sp %.4f bp %.4f",
		sourcef, pricef, s.atr.Last(), lowf, highf, s.lowestPrice, s.highestPrice, s.sellPrice, s.buyPrice)
	// Notify will parse args to strings and process separately
	bbgo.Notify("balances: [Total] %v %s [Base] %s(%v %s) [Quote] %s",
		s.CalcAssetValue(price),
		s.Market.QuoteCurrency,
		balances[s.Market.BaseCurrency].String(),
		balances[s.Market.BaseCurrency].Total().Mul(price),
		s.Market.QuoteCurrency,
		balances[s.Market.QuoteCurrency].String(),
	)

	adx := s.dmi.ADX.Last()
	dip := s.dmi.GetDIPlus().Last()
	dim := s.dmi.GetDIMinus().Last()

	/*1,adx > 20
	2,价格在ema 之上
	3,atr 1,10
	4,量比 rsivol   = ta.rsi(volume, 14)
	    osc      = ta.hma(rsivol, 10)
	*/

	volumeBreak := s.hma.Last() > 49 //.Compare(fixedpoint.NewFromFloat(s.emaSlow.Last()))

	atrdiff := s.atr.Last() > s.atr2.Last()

	up := s.macd.Dif.Index(0) > s.macd.Dea.Index(0) && s.macd.Dif.Index(1) < s.macd.Dea.Index(1)
	down := s.macd.Dif.Index(0) < s.macd.Dea.Index(0) && s.macd.Dif.Index(1) > s.macd.Dea.Index(1)
	long := up && (adx > 20) && (dim > dip)
	sig := s.change.Last()
	if long {
		sig = BUY
	}
	short := down && (adx > 20) && (dim > dip) // && atrdiff && volumeBreak //&& !aboveEma //&& !aboveEma  && volumeBreak
	if short {
		sig = SELL
	}

	///fmt.Println("front", s.change.Last())
	s.change.Push(sig)
	////fmt.Println("back", s.change.Last())
	changed := s.change.Change()
	if changed {
		s.holdingCounter = 0
	} else {
		s.holdingCounter++
	}

	//longCondition := s.rsi.Last() > 50 && s.emaFast.Last() < s.emaSlow.Last() && buleBar && side == types.SideTypeBuy
	//longCondition := changed && s.change.Last() == BUY
	longCondition := changed && s.change.Last() == BUY

	//shortCondition := s.rsi.Last() < 50 && s.emaFast.Last() > s.emaSlow.Last() && redBar && side == types.SideTypeSell
	shortCondition := changed && s.change.Last() == SELL

	closeLong := (changed && s.change.Last() == SELL) // || (s.change.Last() == BUY && s.holdingCounter == holdingMax && !changed) //|| types.CrossUnder(s.dmi.GetDIPlus(), s.dmi.GetDIMinus()).Last()

	closeShort := (changed && s.change.Last() == BUY)
	exitLongCondition := s.buyPrice > 0 && !changed && (s.buyPrice*(1.-stoploss) >= lowf ||
		s.trailingCheck(pricef, "long")) || closeLong

	exitShortCondition := s.sellPrice > 0 && !changed && (s.sellPrice*(1.+stoploss) <= highf ||
		s.trailingCheck(pricef, "short")) || closeShort

	fmt.Printf("time%s,close%.4f,dip:%.4f,dim:%.4f,adx:%.4f\n", kline.EndTime, kline.Close.Float64(), s.dmi.GetDIPlus().Last(), s.dmi.GetDIMinus().Last(), s.dmi.GetADX().Last())
	//fmt.Printf("ema:%.4f,hma:%.4f \n", s.emaSlow.Last(), s.hma.Last())

	//fmt.Printf("macd %.4f dif：%.4f,dea %.4f   \n", s.macd.Last(), s.macd.Dif.Last(), s.macd.Dea.Last())
	fmt.Printf(" macd:%.4f  signal:%.4f,adx%.4f,dip%.4f,dim %.4f\n", s.macd.Difs(), s.macd.Deas(), adx, dip, dim)

	//fmt.Printf("hma:%.4f,rsi:%.4f,adx %.4f \n", s.hma.Last(), s.rsi.Last(), s.dmi.ADX.Last())
	fmt.Printf("atr1:%.4f,atr2:%.4f \n", s.atr.Last(), s.atr2.Last())
	fmt.Printf("changed:%t,changlast:%s, hoding:%d \n", changed, s.change.Last(), s.holdingCounter)
	fmt.Printf("s.currentTakeProfitPrice:%.4f,s.currentStopLossPrice:%.4f,  \n", s.currentTakeProfitPrice.Float64(), s.currentStopLossPrice.Float64())
	//fmt.Printf("stSignal:%d, super last %.4f  \n", stSignal, s.Supertrend.Last())
	fmt.Printf("atrdiff:%t,volumeBreak:%t,  \n", atrdiff, volumeBreak)
	fmt.Printf("longCondition:%t,shortCondition:%t, exitLongCondition:%t,exitShortCondition:%t \n", longCondition, shortCondition, exitLongCondition, exitShortCondition)
	fmt.Println("nimma :", s.positionLock)
	if exitShortCondition {
		if err := s.GeneralOrderExecutor.GracefulCancel(ctx); err != nil {
			log.WithError(err).Errorf("cannot cancel orders")
			s.positionLock.Unlock()
			return
		}
		_ = s.ClosePosition(ctx, fixedpoint.One)
		s.positionLock.Lock()

	}
	//
	if longCondition {
		if err := s.GeneralOrderExecutor.GracefulCancel(ctx); err != nil {
			log.WithError(err).Errorf("cannot cancel orders")
			s.positionLock.Unlock()
			return
		}

		/*source = source.Sub(fixedpoint.NewFromFloat(s.stdevLow.Last() * s.HighLowVarianceMultiplier))
		if source.Compare(price) > 0 {
			source = price
		}*/
		source = fixedpoint.NewFromFloat(s.ma.Last() - s.stdevLow.Last()*s.HighLowVarianceMultiplier)
		if source.Compare(price) > 0 {
			source = price
		}

		sourcef = source.Float64()
		log.Infof("source in long %v %v %f", source, price, s.stdevLow.Last())

		s.positionLock.Unlock()
		opt := s.OpenPositionOptions
		opt.Long = true
		opt.Price = source
		opt.Tags = []string{"long"}
		createdOrders, err := s.GeneralOrderExecutor.OpenPosition(ctx, opt)
		if err != nil {
			if _, ok := err.(types.ZeroAssetError); ok {
				return
			}
			log.WithError(err).Errorf("cannot place buy order")
			return
		}
		log.Infof("orders %v", createdOrders)
		if createdOrders != nil {
			s.orderPendingCounter[createdOrders[0].OrderID] = s.minutesCounter
		}
		return
	}
	//if shortCondition {
	//	if err := s.GeneralOrderExecutor.GracefulCancel(ctx); err != nil {
	//		log.WithError(err).Errorf("cannot cancel orders")
	//		s.positionLock.Unlock()
	//		return
	//	}
	//
	//	source = fixedpoint.NewFromFloat(s.ma.Last() + s.stdevHigh.Last()*s.HighLowVarianceMultiplier)
	//	if source.Compare(price) < 0 {
	//		source = price
	//	}
	//	sourcef = source.Float64()
	//
	//	log.Infof("source in short: %v", source)
	//
	//	s.positionLock.Unlock()
	//	opt := s.OpenPositionOptions
	//	opt.Short = true
	//	opt.Price = source
	//	opt.Tags = []string{"short"}
	//	createdOrders, err := s.GeneralOrderExecutor.OpenPosition(ctx, opt)
	//
	//	fmt.Println("createOrders:", createdOrders, err)
	//	if err != nil {
	//		if _, ok := err.(types.ZeroAssetError); ok {
	//			return
	//		}
	//		log.WithError(err).Errorf("cannot place buy order")
	//		return
	//	}
	//	log.Infof("orders %v", createdOrders)
	//	if createdOrders != nil {
	//		s.orderPendingCounter[createdOrders[0].OrderID] = s.minutesCounter
	//	}
	//	return
	//}
	s.positionLock.Unlock()
}

func (s *Strategy) Run(ctx context.Context, orderExecutor bbgo.OrderExecutor, session *bbgo.ExchangeSession) error {
	if s.Leverage == fixedpoint.Zero {
		s.Leverage = fixedpoint.One
	}
	instanceID := s.InstanceID()
	// Will be set by persistence if there's any from DB
	if s.Position == nil {
		s.Position = types.NewPositionFromMarket(s.Market)
		s.p = types.NewPositionFromMarket(s.Market)
	} else {
		s.p = types.NewPositionFromMarket(s.Market)
		s.p.Base = s.Position.Base
		s.p.Quote = s.Position.Quote
		s.p.AverageCost = s.Position.AverageCost
	}
	if s.ProfitStats == nil {
		s.ProfitStats = types.NewProfitStats(s.Market)
	}
	if s.TradeStats == nil {
		s.TradeStats = types.NewTradeStats(s.Symbol)
	}
	// StrategyController
	s.Status = types.StrategyStatusRunning

	s.OnSuspend(func() {
		_ = s.GeneralOrderExecutor.GracefulCancel(ctx)
	})

	s.OnEmergencyStop(func() {
		_ = s.GeneralOrderExecutor.GracefulCancel(ctx)
		_ = s.ClosePosition(ctx, fixedpoint.One)
	})

	s.GeneralOrderExecutor = bbgo.NewGeneralOrderExecutor(session, s.Symbol, ID, instanceID, s.Position)
	s.GeneralOrderExecutor.BindEnvironment(s.Environment)
	s.GeneralOrderExecutor.BindProfitStats(s.ProfitStats)
	s.GeneralOrderExecutor.BindTradeStats(s.TradeStats)
	s.GeneralOrderExecutor.TradeCollector().OnPositionUpdate(func(position *types.Position) {
		bbgo.Sync(s)
	})
	s.GeneralOrderExecutor.Bind()

	s.orderPendingCounter = make(map[uint64]int)
	s.minutesCounter = 0

	// Exit methods from config
	for _, method := range s.ExitMethods {
		method.Bind(session, s.GeneralOrderExecutor)
	}

	profit := floats.Slice{1., 1.}
	price, _ := s.Session.LastPrice(s.Symbol)
	initAsset := s.CalcAssetValue(price).Float64()
	cumProfit := floats.Slice{initAsset, initAsset}
	modify := func(p float64) float64 {
		return p
	}
	if s.GraphPNLDeductFee {
		modify = func(p float64) float64 {
			return p * (1. - Fee)
		}
	}
	s.GeneralOrderExecutor.TradeCollector().OnTrade(func(trade types.Trade, _profit, _netProfit fixedpoint.Value) {
		s.p.AddTrade(trade)
		order, ok := s.GeneralOrderExecutor.TradeCollector().OrderStore().Get(trade.OrderID)
		if !ok {
			panic(fmt.Sprintf("cannot find order: %v", trade))
		}
		tag := order.Tag

		price := trade.Price.Float64()

		if s.buyPrice > 0 {
			profit.Update(modify(price / s.buyPrice))
			cumProfit.Update(s.CalcAssetValue(trade.Price).Float64())
		} else if s.sellPrice > 0 {
			profit.Update(modify(s.sellPrice / price))
			cumProfit.Update(s.CalcAssetValue(trade.Price).Float64())
		}
		s.positionLock.Lock()
		defer s.positionLock.Unlock()
		// tag == "" is for exits trades
		if tag == "close" || tag == "" {
			if s.p.IsDust(trade.Price) {
				s.buyPrice = 0
				s.sellPrice = 0
				s.highestPrice = 0
				s.lowestPrice = 0
			} else if s.p.IsLong() {
				s.sellPrice = 0
				s.lowestPrice = 0
			} else {
				s.buyPrice = 0
				s.highestPrice = 0
			}
		} else if tag == "long" {
			if s.p.IsDust(trade.Price) {
				s.buyPrice = 0
				s.sellPrice = 0
				s.highestPrice = 0
				s.lowestPrice = 0
			} else if s.p.IsLong() {
				s.buyPrice = trade.Price.Float64()
				s.sellPrice = 0
				s.highestPrice = s.buyPrice
				s.lowestPrice = 0
			}
		} else if tag == "short" {
			if s.p.IsDust(trade.Price) {
				s.sellPrice = 0
				s.buyPrice = 0
				s.highestPrice = 0
				s.lowestPrice = 0
			} else if s.p.IsShort() {
				s.sellPrice = trade.Price.Float64()
				s.buyPrice = 0
				s.highestPrice = 0
				s.lowestPrice = s.sellPrice
			}
		} else {
			panic("tag unknown")
		}
		bbgo.Notify("tag: %s, sp: %.4f bp: %.4f hp: %.4f lp: %.4f, trade: %s, pos: %s", tag, s.sellPrice, s.buyPrice, s.highestPrice, s.lowestPrice, trade.String(), s.p.String())
	})

	s.frameKLine = &types.KLine{}
	s.kline1m = &types.KLine{}
	s.priceLines = types.NewQueue(300)

	s.initTickerFunctions(ctx)
	startTime := s.Environment.StartTime()
	s.TradeStats.SetIntervalProfitCollector(types.NewIntervalProfitCollector(types.Interval30m, startTime))
	s.TradeStats.SetIntervalProfitCollector(types.NewIntervalProfitCollector(types.Interval1h, startTime))
	s.TradeStats.SetIntervalProfitCollector(types.NewIntervalProfitCollector(types.Interval1d, startTime))
	s.TradeStats.SetIntervalProfitCollector(types.NewIntervalProfitCollector(types.Interval1w, startTime))

	// default value: use 1m kline
	if !s.NoTrailingStopLoss && s.IsBackTesting() || s.TrailingStopLossType == "" {
		s.TrailingStopLossType = "kline"
	}

	bbgo.RegisterCommand("/draw", "Draw Indicators", func(reply interact.Reply) {
		canvas := s.DrawIndicators(s.frameKLine.StartTime)
		var buffer bytes.Buffer
		if err := canvas.Render(chart.PNG, &buffer); err != nil {
			log.WithError(err).Errorf("cannot render indicators in drift")
			reply.Message(fmt.Sprintf("[error] cannot render indicators in drift: %v", err))
			return
		}
		bbgo.SendPhoto(&buffer)
	})

	bbgo.RegisterCommand("/pnl", "Draw PNL(%) per trade", func(reply interact.Reply) {
		canvas := s.DrawPNL(&profit)
		var buffer bytes.Buffer
		if err := canvas.Render(chart.PNG, &buffer); err != nil {
			log.WithError(err).Errorf("cannot render pnl in drift")
			reply.Message(fmt.Sprintf("[error] cannot render pnl in drift: %v", err))
			return
		}
		bbgo.SendPhoto(&buffer)
	})

	bbgo.RegisterCommand("/cumpnl", "Draw Cummulative PNL(Quote)", func(reply interact.Reply) {
		canvas := s.DrawCumPNL(&cumProfit)
		var buffer bytes.Buffer
		if err := canvas.Render(chart.PNG, &buffer); err != nil {
			log.WithError(err).Errorf("cannot render cumpnl in drift")
			reply.Message(fmt.Sprintf("[error] canot render cumpnl in drift: %v", err))
			return
		}
		bbgo.SendPhoto(&buffer)
	})

	bbgo.RegisterCommand("/config", "Show latest config", func(reply interact.Reply) {
		var buffer bytes.Buffer
		s.Print(&buffer, false)
		reply.Message(buffer.String())
	})

	bbgo.RegisterCommand("/pos", "Show internal position", func(reply interact.Reply) {
		reply.Message(s.p.String())
	})

	bbgo.RegisterCommand("/dump", "Dump internal params", func(reply interact.Reply) {
		reply.Message("Please enter series output length:")
	}).Next(func(length string, reply interact.Reply) {
		var buffer bytes.Buffer
		l, err := strconv.Atoi(length)
		if err != nil {
			s.ParamDump(&buffer)
		} else {
			s.ParamDump(&buffer, l)
		}
		reply.Message(buffer.String())
	})

	bbgo.RegisterModifier(s)

	// event trigger order: s.Interval => Interval1m
	store, ok := session.MarketDataStore(s.Symbol)

	//store, ok := session.SerialMarketDataStore(s.Symbol, []types.Interval{s.Interval, types.Interval1m})
	if !ok {
		panic("cannot get 1m history")
	}
	if err := s.initIndicators(store); err != nil {
		log.WithError(err).Errorf("initIndicator failed")
		return nil
	}
	store.OnKLineClosed(func(kline types.KLine) {
		s.minutesCounter = int(kline.StartTime.Time().Add(kline.Interval.Duration()).Sub(s.startTime).Minutes())
		if kline.Interval == types.Interval1m {
			s.klineHandler1m(ctx, kline)
		} else if kline.Interval == s.Interval {
			s.klineHandler(ctx, kline)
		}
	})

	bbgo.OnShutdown(func(ctx context.Context, wg *sync.WaitGroup) {

		var buffer bytes.Buffer

		s.Print(&buffer, true, true)

		fmt.Fprintln(&buffer, "--- NonProfitable Dates ---")
		for _, daypnl := range s.TradeStats.IntervalProfits[types.Interval1d].GetNonProfitableIntervals() {
			fmt.Fprintf(&buffer, "%s\n", daypnl)
		}
		fmt.Fprintln(&buffer, s.TradeStats.BriefString())

		os.Stdout.Write(buffer.Bytes())

		if s.GenerateGraph {
			s.Draw(s.frameKLine.StartTime, &profit, &cumProfit)
		}
		wg.Done()
	})
	return nil
}
