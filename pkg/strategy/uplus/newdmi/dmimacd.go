package newdmi

import (
	"context"
	"errors"
	"fmt"
	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/datatype/floats"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/indicator"
	"github.com/c9s/bbgo/pkg/strategy/uplus/indi"
	"github.com/c9s/bbgo/pkg/types"
	"github.com/c9s/bbgo/pkg/util"
	"github.com/davecgh/go-spew/spew"

	"sync"
)

type MACDDivergence struct {
	*indicator.MACDConfig
	PivotWindow int `json:"pivotWindow"`
}

// DmiMacd -- when price breaks the previous pivot low, we set a trade entry
type DmiMacd struct {
	Symbol string
	Market types.Market
	types.IntervalWindow

	// FastWindow is used for fast pivot (this is to to filter the nearest high/low)
	FastWindow int `json:"fastWindow"`

	// Ratio is a number less than 1.0, price * ratio will be the price triggers the short order.
	Ratio fixedpoint.Value `json:"ratio"`

	// MarketOrder is the option to enable market order short.
	MarketOrder bool `json:"marketOrder"`

	// LimitOrder is the option to use limit order instead of market order to short
	LimitOrder      bool             `json:"limitOrder"`
	LimitTakerRatio fixedpoint.Value `json:"limitTakerRatio"`
	Leverage        fixedpoint.Value `json:"leverage"`
	Quantity        fixedpoint.Value `json:"quantity"`

	bbgo.OpenPositionOptions

	// BounceRatio is a ratio used for placing the limit order sell price
	// limit sell price = DmiMacdPrice * (1 + BounceRatio)
	BounceRatio fixedpoint.Value `json:"bounceRatio"`

	StopEMA *bbgo.StopEMA `json:"stopEMA"`

	TrendEMA *bbgo.TrendEMA `json:"trendEMA"`

	lastLow, lastFastLow fixedpoint.Value

	// lastDmiMacd is the low that the price just break
	lastDmiMacd fixedpoint.Value

	pivotLow, fastPivotLow *indicator.PivotLow
	pivotLowPrices         []fixedpoint.Value

	trendEWMALast, trendEWMACurrent float64

	orderExecutor *bbgo.GeneralOrderExecutor
	session       *bbgo.ExchangeSession

	// StrategyController
	bbgo.StrategyController

	bbgo.SourceSelector

	*bbgo.Environment

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
	MaType      string           `json:"maType,omitempty"`
	//ewo        *ElliottWave
	emaFast *indicator.EWMA
	emaSlow *indicator.EWMA
	alma    *indicator.ALMA
	dema    *indicator.DEMA
	ma      *indicator.SMA

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

	atr   *indicator.ATR
	atr2  *indicator.ATR
	stoch *indicator.STOCH

	// for position
	buyPrice     float64 `persistence:"buy_price"`
	sellPrice    float64 `persistence:"sell_price"`
	highestPrice float64 `persistence:"highest_price"`
	lowestPrice  float64 `persistence:"lowest_price"`

	getLastPrice func() fixedpoint.Value

	currentTakeProfitPrice fixedpoint.Value
	currentStopLossPrice   fixedpoint.Value

	midPrice fixedpoint.Value
	lock     sync.RWMutex `ignore:"true"`

	// TakeProfitAtrMultiplier TP according to ATR multiple, 0 to disable this
	TakeProfitAtrMultiplier float64 `json:"takeProfitAtrMultiplier"`
}

func (s *DmiMacd) Subscribe(session *bbgo.ExchangeSession) {
	session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{Interval: s.Interval})
	session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{Interval: types.Interval1m})

	if s.StopEMA != nil {
		session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{Interval: s.StopEMA.Interval})
	}

	if s.TrendEMA != nil {
		session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{Interval: s.TrendEMA.Interval})
	}

}

func (s *DmiMacd) initIndicators(store *bbgo.MarketDataStore) error {

	s.change = &indi.Slice{}

	s.emaFast = &indicator.EWMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSlow.Int()}}
	s.emaSlow = &indicator.EWMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowQuick}}
	s.dmi = &indicator.DMI{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowDmi}, ADXSmoothing: s.WindowDmi}

	s.stoch = &indicator.STOCH{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSTOCH}}

	s.atr = &indicator.ATR{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowATR}}
	s.atr2 = &indicator.ATR{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowATR2}}
	s.rsi = &indicator.RSI{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowRSI}}
	s.cci = &indicator.CCI{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowCCI}}
	s.alma = &indicator.ALMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSlow.Int()}, Offset: 0.775, Sigma: 5}
	s.dema = &indicator.DEMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSlow.Int()}}
	s.ma = &indicator.SMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSlow.Int()}}
	s.hull = &indicator.HULL{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSlow.Int()}}

	s.macd = &DEMACD{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowMACD}, ShortPeriod: 9, LongPeriod: 26, DeaPeriod: 12, MaType: "EWMA"}

	s.hma = &indi.HMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowHMA}}

	if s.SupertrendMultiplier == 0 {
		s.SupertrendMultiplier = 3
	}
	s.Supertrend = &indicator.Supertrend{IntervalWindow: types.IntervalWindow{Window: s.Window, Interval: s.Interval}, ATRMultiplier: s.SupertrendMultiplier}
	s.Supertrend.AverageTrueRange = &indicator.ATR{IntervalWindow: types.IntervalWindow{Window: s.Window, Interval: s.Interval}}
	s.Supertrend.BindK(s.session.MarketDataStream, s.Symbol, s.Supertrend.Interval)

	klines, ok := store.KLinesOfInterval(s.Interval)
	klineLength := len(*klines)
	if !ok || klineLength == 0 {
		return errors.New("klines not exists")
	}

	for _, kline := range *klines {
		s.UpdateKline(kline)

	}
	s.Supertrend.LoadK((*klines)[0:])
	return nil
}
func (s *DmiMacd) UpdateKline(kline types.KLine) {
	source := s.GetSource(&kline).Float64()

	s.atr.PushK(kline)
	s.atr2.PushK(kline)

	s.rsi.Update(kline.Volume.Float64())
	s.hma.Update(s.rsi.Last())

	s.cci.PushK(kline)

	s.alma.Update(source)
	s.emaFast.PushK(kline)
	s.emaSlow.PushK(kline)
	s.dema.PushK(kline)
	s.ma.PushK(kline)
	s.hull.PushK(kline)

	s.stoch.PushK(kline)

	s.dmi.PushK(kline)
	s.macd.PushK(kline)

	fmt.Println(kline)

	fmt.Printf("macd %.4f dif：%.4f,dea %.4f alma, %.4f \n", s.macd.Last(), s.macd.Dif.Last(), s.macd.Dea.Last(), s.alma.Last())

	fmt.Printf("rsi %.4f cci：%.4f,atr %.4f  \n", s.rsi.Last(), s.cci.Last(), s.atr.Last())
	fmt.Printf("rsi %.4f cci：%.4f,atr %.4f  \n", s.rsi.Last(), s.cci.Last(), s.atr.Last())
	fmt.Printf("time%s,close%.4f,dip:%.4f,dim:%.4f,adx:%.4f\n", kline.EndTime, kline.Close.Float64(), s.dmi.GetDIPlus().Last(), s.dmi.GetDIMinus().Last(), s.dmi.GetADX().Last())
	fmt.Printf("ema:%.4f,hma:%.4f \n", s.emaSlow.Last(), s.hma.Last())
	fmt.Printf("hma:%.4f,rsi:%.4f \n", s.hma.Last(), s.rsi.Last())
	fmt.Printf("atr1:%.4f,atr2:%.4f  \n", s.atr.Last(), s.atr2.Last())
}

func (s *DmiMacd) CalcAssetValue(price fixedpoint.Value) fixedpoint.Value {
	balances := s.session.GetAccount().Balances()
	return balances[s.Market.BaseCurrency].Total().Mul(price).Add(balances[s.Market.QuoteCurrency].Total())
}

func (s *DmiMacd) initTickerFunctions() {
	if s.IsBackTesting() {
		s.getLastPrice = func() fixedpoint.Value {
			lastPrice, ok := s.session.LastPrice(s.Symbol)
			if !ok {
				log.Error("cannot get lastprice")
			}
			return lastPrice
		}
	} else {
		s.session.MarketDataStream.OnBookTickerUpdate(func(ticker types.BookTicker) {
			bestBid := ticker.Buy
			bestAsk := ticker.Sell
			if !util.TryLock(&s.lock) {
				return
			}
			if !bestAsk.IsZero() && !bestBid.IsZero() {
				s.midPrice = bestAsk.Add(bestBid).Div(Two)
			} else if !bestAsk.IsZero() {
				s.midPrice = bestAsk
			} else if !bestBid.IsZero() {
				s.midPrice = bestBid
			}
			s.lock.Unlock()
		})
		s.getLastPrice = func() (lastPrice fixedpoint.Value) {
			var ok bool
			s.lock.RLock()
			defer s.lock.RUnlock()
			if s.midPrice.IsZero() {
				lastPrice, ok = s.session.LastPrice(s.Symbol)
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

func (s *DmiMacd) Bind(session *bbgo.ExchangeSession, orderExecutor *bbgo.GeneralOrderExecutor) {

	s.session = session
	s.orderExecutor = orderExecutor

	// StrategyController
	s.Status = types.StrategyStatusRunning
	store, ok := session.MarketDataStore(s.Symbol)
	s.currentStopLossPrice = fixedpoint.Zero
	s.currentTakeProfitPrice = fixedpoint.Zero

	if !ok {
		panic("cannot get 1m history")
	}
	if err := s.initIndicators(store); err != nil {
		log.WithError(err).Errorf("initIndicator failed")

	}
	s.initTickerFunctions()

	position := s.orderExecutor.Position()
	symbol := position.Symbol

	profit := floats.Slice{1., 1.}
	price, _ := s.session.LastPrice(s.Symbol)
	initAsset := s.CalcAssetValue(price).Float64()
	cumProfit := floats.Slice{initAsset, initAsset}
	modify := func(p float64) float64 {
		return p
	}
	s.orderExecutor.TradeCollector().OnTrade(func(trade types.Trade, _profit, _netProfit fixedpoint.Value) {
		spew.Dump(trade)
		price := trade.Price.Float64()
		if s.buyPrice > 0 {
			profit.Update(modify(price / s.buyPrice))
			cumProfit.Update(s.CalcAssetValue(trade.Price).Float64())
		} else if s.sellPrice > 0 {
			profit.Update(modify(s.sellPrice / price))
			cumProfit.Update(s.CalcAssetValue(trade.Price).Float64())
		}

		if s.orderExecutor.Position().IsDust(trade.Price) {
			s.buyPrice = 0
			s.sellPrice = 0
			s.highestPrice = 0
			s.lowestPrice = 0
		} else if s.orderExecutor.Position().IsLong() {
			s.buyPrice = price
			s.sellPrice = 0
			s.highestPrice = s.buyPrice
			s.lowestPrice = 0
		} else {
			s.sellPrice = price
			s.buyPrice = 0
			s.highestPrice = 0
			s.lowestPrice = s.sellPrice
		}
	})

	session.MarketDataStream.OnStart(func() {

		s.pilotQuantityCalculation()
	})

	//var buy bool
	session.MarketDataStream.OnKLine(func(kline types.KLine) {
		//
		//ctx := context.Background()
		//opts := s.OpenPositionOptions
		//opts.Short = true
		//opts.Price = kline.Close
		//opts.Tags = []string{"breakLowMarket"}
		//
		//fmt.Println("\n::::postion:", position.PlainText())
		//fmt.Println(position.IsOpened(kline.Close))
		//if position.IsOpened(kline.Close) {
		//	bbgo.Notify("position is already opened, skip")
		//	fmt.Println("position is already opened, skip33")
		//	time.Sleep(1)
		//	err := s.orderExecutor.ClosePosition(ctx, fixedpoint.One)
		//	fmt.Println(err)
		//	return
		//} else {
		//	fmt.Println("position is closed opened, skip")
		//
		//	if _, err := s.orderExecutor.OpenPosition(ctx, opts); err != nil {
		//		log.WithError(err).Errorf("failed to open short position")
		//	}
		//	time.Sleep(5 * time.Millisecond)
		//
		//}
		////

		//buy = true
		//opts.Params = param

		//if opts.LimitOrder && !s.BounceRatio.IsZero() {
		//	opts.Price = previousLow.Mul(fixedpoint.One.Add(s.BounceRatio))
		//}

		//fmt.Printf("time%s,close%.4f,dip:%.4f,dim:%.4f,adx:%.4f\n", kline.EndTime, kline.Close.Float64(), s.dmi.GetDIPlus().Last(), s.dmi.GetDIMinus().Last(), s.dmi.GetADX().Last())
		//fmt.Printf("ema:%.4f,hma:%.4f \n", s.emaSlow.Last(), s.hma.Last())
		//fmt.Printf("hma:%.4f,rsi:%.4f \n", s.hma.Last(), s.rsi.Last())
		//fmt.Printf("atr1:%.4f,atr2:%.4f  \n", s.atr.Last(), s.atr2.Last())
	})
	session.MarketDataStream.OnKLine(func(kline types.KLine) {
		//ctx := context.Background()
		//
		//err := s.orderExecutor.ClosePosition(ctx, fixedpoint.One)
		//fmt.Println(err)
		//fmt.Printf("time%s,close%.4f,dip:%.4f,dim:%.4f,adx:%.4f\n", kline.EndTime, kline.Close.Float64(), s.dmi.GetDIPlus().Last(), s.dmi.GetDIMinus().Last(), s.dmi.GetADX().Last())
		//fmt.Printf("ema:%.4f,hma:%.4f \n", s.emaSlow.Last(), s.hma.Last())
		//fmt.Printf("hma:%.4f,rsi:%.4f \n", s.hma.Last(), s.rsi.Last())
		//fmt.Printf("atr1:%.4f,atr2:%.4f  \n", s.atr.Last(), s.atr2.Last())
	})

	session.MarketDataStream.OnKLineClosed(types.KLineWith(symbol, s.Interval, func(kline types.KLine) {
		s.UpdateKline(kline)
		s.Place(kline)

	}))

	session.MarketDataStream.OnKLineClosed(types.KLineWith(s.Symbol, types.Interval1m, func(kline types.KLine) {

	}))
}

// 处理逻辑
func (s *DmiMacd) Place(kline types.KLine) {
	source := s.GetSource(&kline)
	closePrice := kline.GetClose()
	price := s.getLastPrice()
	//sourcef := source.Float64()
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

	//maPrice := 0.0
	//switch s.MaType {
	//case "ema":
	//	maPrice = s.emaSlow.Last()
	//case "sma":
	//	maPrice = s.ma.Last()
	//case "dema":
	//	maPrice = s.dema.Last()
	//case "alma":
	//	maPrice = s.alma.Last()
	//case "hull":
	//	maPrice = s.hull.Last()
	//
	//}
	//s.macd.MACD()
	//s.macd.Singals()

	//ta.crossover(macd_signal, macd) and pos_dm < neg_dm and avg_dm>25
	/**
	1、macd 下穿信号线
	*/
	long := s.macd.Cross() == CrossOver && (adx > 25 && dip-dim > 5)
	sig := s.change.Last()
	if long {
		sig = BUY
	}
	short := s.macd.Cross() == CrossUnder && (adx > 25 && dim-dip > 5) // && atrdiff && volumeBreak //&& !aboveEma //&& !aboveEma  && volumeBreak
	if short {
		sig = SELL
	}
	fmt.Printf("s.macd.MACD():%.4f s.macd.Singals():%.4f  dea:%.4f,adx%.4f,dip%.4f,dim %.4f\n", s.macd.Last(), s.macd.Difs(), s.macd.Deas(), adx, dip, dim)

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
	longCondition := changed && s.change.Last() == BUY

	//shortCondition := s.rsi.Last() < 50 && s.emaFast.Last() > s.emaSlow.Last() && redBar && side == types.SideTypeSell
	shortCondition := changed && s.change.Last() == SELL

	closeLong := (changed && s.change.Last() == SELL) || (s.change.Last() == BUY && s.holdingCounter == holdingMax && !changed) || types.CrossUnder(s.dmi.GetDIPlus(), s.dmi.GetDIMinus()).Last()

	closeShort := (changed && s.change.Last() == BUY) || (s.change.Last() == SELL && s.holdingCounter == holdingMax && !changed) || types.CrossOver(s.dmi.GetDIPlus(), s.dmi.GetDIMinus()).Last()
	s.currentTakeProfitPrice = closePrice.Add(fixedpoint.NewFromFloat(s.Supertrend.AverageTrueRange.Last() * s.TakeProfitAtrMultiplier))
	s.currentStopLossPrice = closePrice.Sub(fixedpoint.NewFromFloat(s.Supertrend.AverageTrueRange.Last() * s.TakeProfitAtrMultiplier))

	stSignal := s.Supertrend.GetSignal()

	exitLongCondition := s.buyPrice > 0 && !longCondition || closeLong || kline.GetClose().Compare(s.currentTakeProfitPrice) > 0 || stSignal == types.DirectionDown //kline.Close.Float64() > s.buyPrice+1.5*atr //|| s.trailingCheck(lowf, "long")

	exitShortCondition := s.sellPrice > 0 && !shortCondition || closeShort || kline.GetClose().Compare(s.currentStopLossPrice) > 0 || stSignal == types.DirectionUp //|| s.sellPrice+atr <= highf || kline.Close.Float64() < s.sellPrice-1.5*atr //|| s.trailingCheck(highf, "short")
	fmt.Printf("time%s,close%.4f,dip:%.4f,dim:%.4f,adx:%.4f\n", kline.EndTime, kline.Close.Float64(), s.dmi.GetDIPlus().Last(), s.dmi.GetDIMinus().Last(), s.dmi.GetADX().Last())
	fmt.Printf("ema:%.4f,hma:%.4f \n", s.emaSlow.Last(), s.hma.Last())
	fmt.Printf("hma:%.4f,rsi:%.4f,adx %.4f \n", s.hma.Last(), s.rsi.Last(), s.dmi.ADX.Last())
	fmt.Printf("atr1:%.4f,atr2:%.4f \n", s.atr.Last(), s.atr2.Last())
	fmt.Printf("changed:%t,changlast:%s, hoding:%d \n", changed, s.change.Last(), s.holdingCounter)
	fmt.Printf("s.currentTakeProfitPrice:%.4f,s.currentStopLossPrice:%.4f,  \n", s.currentTakeProfitPrice, s.currentStopLossPrice)
	fmt.Printf("stSignal:%s,   \n", stSignal)
	fmt.Printf("atrdiff:%t,volumeBreak:%t,  \n", atrdiff, volumeBreak)
	fmt.Printf("longCondition:%t,shortCondition:%t, exitLongCondition:%t,exitShortCondition:%t \n", longCondition, shortCondition, exitLongCondition, exitShortCondition)

	//fmt.Println(s.sellPrice > 0, s.buyPrice*(1.-stoploss) >= lowf, s.buyPrice-atr >= lowf, s.trailingCheck(lowf, "long"))
	ctx := context.Background()
	if exitShortCondition || exitLongCondition {
		if err := s.orderExecutor.GracefulCancel(ctx); err != nil {
			log.WithError(err).Errorf("cannot cancel orders")
			return
		}
		s.orderExecutor.ClosePosition(ctx, fixedpoint.One)
	}

	//shortCondition = true
	if longCondition {
		if err := s.orderExecutor.GracefulCancel(ctx); err != nil {
			log.WithError(err).Errorf("cannot cancel orders")
			return
		}
		if source.Compare(price) > 0 {
			source = price

		}
		//bbgo.Notify("%s price %f breaks the previous low %f with ratio %f, opening short position", symbol, kline.Close.Float64(), previousLow.Float64(), s.Ratio.Float64())
		opts := s.OpenPositionOptions
		opts.Long = true
		opts.Price = source
		opts.Tags = []string{"breakLowMarket"}

		//if opts.LimitOrder && !s.BounceRatio.IsZero() {
		//	opts.Price = previousLow.Mul(fixedpoint.One.Add(s.BounceRatio))
		//}

		if _, err := s.orderExecutor.OpenPosition(ctx, opts); err != nil {
			log.WithError(err).Errorf("failed to open short position")
		}
	}
	if shortCondition {
		if err := s.orderExecutor.GracefulCancel(ctx); err != nil {
			log.WithError(err).Errorf("cannot cancel orders")
			return
		}
		if source.Compare(price) > 0 {
			source = price

		}
		//bbgo.Notify("%s price %f breaks the previous low %f with ratio %f, opening short position", symbol, kline.Close.Float64(), previousLow.Float64(), s.Ratio.Float64())
		opts := s.OpenPositionOptions
		opts.Short = true
		opts.Price = source
		opts.Tags = []string{"breakLowMarket"}

		//if opts.LimitOrder && !s.BounceRatio.IsZero() {
		//	opts.Price = previousLow.Mul(fixedpoint.One.Add(s.BounceRatio))
		//}

		if _, err := s.orderExecutor.OpenPosition(ctx, opts); err != nil {
			log.WithError(err).Errorf("failed to open short position")
		}

	}

}

func (s *DmiMacd) pilotQuantityCalculation() {
	if s.lastLow.IsZero() {
		return
	}

	log.Infof("pilot calculation for max position: last low = %f, quantity = %f, leverage = %f",
		s.lastLow.Float64(),
		s.Quantity.Float64(),
		s.Leverage.Float64())

	quantity, err := bbgo.CalculateBaseQuantity(s.session, s.Market, s.lastLow, s.Quantity, s.Leverage)
	if err != nil {
		log.WithError(err).Errorf("quantity calculation error")
	}

	if quantity.IsZero() {
		log.WithError(err).Errorf("quantity is zero, can not submit order")
		return
	}

	bbgo.Notify("%s %f quantity will be used for shorting", s.Symbol, quantity.Float64())
}
