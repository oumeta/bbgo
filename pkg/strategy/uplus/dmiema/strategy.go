package dmiema

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/datatype/floats"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/indicator"
	"github.com/c9s/bbgo/pkg/types"
	"github.com/c9s/bbgo/pkg/util"
	"github.com/sirupsen/logrus"
)

const ID = "dmiema"

var log = logrus.WithField("strategy", ID)
var Two fixedpoint.Value = fixedpoint.NewFromInt(2)
var Three fixedpoint.Value = fixedpoint.NewFromInt(3)
var Four fixedpoint.Value = fixedpoint.NewFromInt(4)
var Delta fixedpoint.Value = fixedpoint.NewFromFloat(0.00001)
var BUY = "1"
var SELL = "-1"
var HOLD = "0"
var holdingMax = 5

func init() {
	bbgo.RegisterStrategy(ID, &Strategy{})
}

type SourceFunc func(*types.KLine) fixedpoint.Value

type Strategy struct {
	Symbol string `json:"symbol"`

	bbgo.StrategyController
	bbgo.SourceSelector
	types.Market
	Session *bbgo.ExchangeSession
	bbgo.OpenPositionOptions

	Interval    types.Interval   `json:"interval"`
	Stoploss    fixedpoint.Value `json:"stoploss"`
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

	PendingMinutes int  `json:"pendingMinutes"`
	UseHeikinAshi  bool `json:"useHeikinAshi"`

	// whether to draw graph or not by the end of backtest
	DrawGraph          bool   `json:"drawGraph"`
	GraphIndicatorPath string `json:"graphIndicatorPath"`
	GraphPNLPath       string `json:"graphPNLPath"`
	GraphCumPNLPath    string `json:"graphCumPNLPath"`

	*bbgo.Environment
	*bbgo.GeneralOrderExecutor
	*types.Position    `persistence:"position"`
	*types.ProfitStats `persistence:"profit_stats"`
	*types.TradeStats  `persistence:"trade_stats"`
	MaType             string `json:"maType,omitempty"`
	//ewo        *ElliottWave
	emaFast    *indicator.EWMA
	emaSlow    *indicator.EWMA
	alma       *indicator.ALMA
	dema       *indicator.DEMA
	ma         *indicator.SMA
	rsima      *RsiMA
	rsi        *indicator.RSI
	cci        *indicator.CCI
	dmi        *indicator.DMI
	hma        *HMA
	hull       *indicator.HULL
	change     *Slice
	macd       *DEMACD
	atr        *indicator.ATR
	atr2       *indicator.ATR
	stoch      *indicator.STOCH
	heikinAshi *HeikinAshi

	priceLines *types.Queue

	getLastPrice func() fixedpoint.Value

	// for smart cancel
	orderPendingCounter map[uint64]int
	startTime           time.Time
	minutesCounter      int
	holdingCounter      int

	// for position
	buyPrice     float64 `persistence:"buy_price"`
	sellPrice    float64 `persistence:"sell_price"`
	highestPrice float64 `persistence:"highest_price"`
	lowestPrice  float64 `persistence:"lowest_price"`

	TrailingCallbackRate    []float64          `json:"trailingCallbackRate"`
	TrailingActivationRatio []float64          `json:"trailingActivationRatio"`
	ExitMethods             bbgo.ExitMethodSet `json:"exits"`

	midPrice fixedpoint.Value
	lock     sync.RWMutex `ignore:"true"`
}

func (s *Strategy) ID() string {
	return ID
}

func (s *Strategy) InstanceID() string {
	return fmt.Sprintf("%s:%s:%v", ID, s.Symbol, bbgo.IsBackTesting)
}

func (s *Strategy) Subscribe(session *bbgo.ExchangeSession) {
	// by default, bbgo only pre-subscribe 1000 klines.
	// this is not enough if we're subscribing 30m intervals using SerialMarketDataStore
	//bbgo.KLinePreloadLimit = int64((s.Interval.Minutes()*s.WindowSlow/1000 + 1) + 1000)
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

func (s *Strategy) ClosePosition(ctx context.Context, percentage fixedpoint.Value) error {
	order := s.Position.NewMarketCloseOrder(percentage)
	fmt.Println(order)
	fmt.Println(s.Position.String())
	fmt.Println(s.Position.Base, s.Position.GetQuantity(), s.Position.Quote)
	fmt.Println("go 1")
	if order == nil {
		return nil
	}
	fmt.Println("go 2")

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

	for {
		fmt.Println("go 3", order.Quantity, price)

		fmt.Println(order.Quantity.Compare(s.Market.MinQuantity) <= 0)
		fmt.Println(order.Quantity.Mul(price).Compare(s.Market.MinNotional) <= 0)

		if s.Market.IsDustQuantity(order.Quantity, price) {
			return nil
		}
		fmt.Println("go 4")

		_, err := s.GeneralOrderExecutor.SubmitOrders(ctx, *order)
		if err != nil {
			order.Quantity = order.Quantity.Mul(fixedpoint.One.Sub(Delta))
			continue
		}
		return nil
	}

}

func (s *Strategy) initIndicators(store *bbgo.MarketDataStore) error {

	s.change = &Slice{}
	s.priceLines = types.NewQueue(300)
	s.emaFast = &indicator.EWMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSlow.Int()}}
	s.emaSlow = &indicator.EWMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowQuick}}
	s.dmi = &indicator.DMI{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowDmi}, ADXSmoothing: s.WindowDmi}

	//s.ewo = &ElliottWave{
	//	maSlow, maQuick,
	//}
	s.stoch = &indicator.STOCH{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSTOCH}}

	s.atr = &indicator.ATR{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowATR}}
	s.atr2 = &indicator.ATR{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowATR2}}
	s.rsi = &indicator.RSI{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowRSI}}
	s.cci = &indicator.CCI{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowCCI}}
	s.alma = &indicator.ALMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSlow.Int()}, Offset: 0.775, Sigma: 4.5}
	s.dema = &indicator.DEMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSlow.Int()}}
	s.ma = &indicator.SMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSlow.Int()}}
	s.hull = &indicator.HULL{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSlow.Int()}}

	s.hma = &HMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowHMA}}
	s.macd = &DEMACD{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowMACD}, ShortPeriod: 9, LongPeriod: 49, DeaPeriod: 9, MaType: "EWMA"}
	s.rsima = &RsiMA{

		rsi: &indicator.RSI{
			IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowRSI},
		},
		rsiMa: &indicator.SMA{
			IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowRSI},
		},
	}
	klines, ok := store.KLinesOfInterval(s.Interval)
	klineLength := len(*klines)
	if !ok || klineLength == 0 {
		return errors.New("klines not exists")
	}
	tmpK := (*klines)[klineLength-1]
	s.startTime = tmpK.StartTime.Time().Add(tmpK.Interval.Duration())
	s.heikinAshi = NewHeikinAshi(500)

	for _, kline := range *klines {
		source := s.GetSource(&kline).Float64()
		//s.ewo.Update(source)

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
		s.macd.PushK(kline)
		s.stoch.PushK(kline)
		s.priceLines.Update(source)
		s.dmi.PushK(kline)
		s.heikinAshi.Update(kline)

		if s.UseHeikinAshi {
			s.heikinAshi.Update(kline)
			source := s.GetSource(s.heikinAshi.Last())
			sourcef := source.Float64()

			s.rsima.Update(sourcef)
			//r, m := s.rsima.Last()
			//fmt.Println("rsi:", r, m)
			//fmt.Println(s.heikinAshi.Last())
		} else {
			s.rsima.Update(kline.Close.Float64())
		}
		//fmt.Println(kline)
		//fmt.Printf("macd %.4f dif：%.4f,dea %.4f alma, %.4f \n", s.macd.Last(), s.macd.Dif.Last(), s.macd.Dea.Last(), s.alma.Last())
		//fmt.Printf("rsi %.4f cci：%.4f,atr %.4f  \n", s.rsi.Last(), s.cci.Last(), s.atr.Last())
		//fmt.Printf("rsi %.4f cci：%.4f,atr %.4f  \n", s.rsi.Last(), s.cci.Last(), s.atr.Last())
		fmt.Printf("time%s,close%.4f,dip:%.4f,dim:%.4f,adx:%.4f\n", kline.EndTime, kline.Close.Float64(), s.dmi.GetDIPlus().Last(), s.dmi.GetDIMinus().Last(), s.dmi.GetADX().Last())
		fmt.Printf("ema:%.4f,hma:%.4f \n", s.emaSlow.Last(), s.hma.Last())
		fmt.Printf("hma:%.4f,rsi:%.4f \n", s.hma.Last(), s.rsi.Last())
		fmt.Printf("atr1:%.4f,atr2:%.4f  \n", s.atr.Last(), s.atr2.Last())

	}
	return nil
}

// FIXME: stdevHigh
func (s *Strategy) smartCancel(ctx context.Context, pricef float64) int {
	nonTraded := s.GeneralOrderExecutor.ActiveMakerOrders().Orders()
	if len(nonTraded) > 0 {
		left := 0
		for _, order := range nonTraded {
			if order.Status != types.OrderStatusNew && order.Status != types.OrderStatusPartiallyFilled {
				continue
			}
			log.Warnf("%v | counter: %d, system: %d", order, s.orderPendingCounter[order.OrderID], s.minutesCounter)
			toCancel := false
			if s.minutesCounter-s.orderPendingCounter[order.OrderID] >= s.PendingMinutes {
				toCancel = true
			} else if order.Side == types.SideTypeBuy {
				if order.Price.Float64()+s.atr.Last()*2 <= pricef {
					toCancel = true
				}
			} else if order.Side == types.SideTypeSell {
				// 75% of the probability
				if order.Price.Float64()-s.atr.Last()*2 >= pricef {
					toCancel = true
				}
			} else {
				panic("not supported side for the order")
			}
			if toCancel {
				err := s.GeneralOrderExecutor.GracefulCancelOrder(ctx, order)
				if err == nil {
					delete(s.orderPendingCounter, order.OrderID)
				} else {
					log.WithError(err).Errorf("failed to cancel %v", order.OrderID)
				}
				log.Warnf("cancel %v", order.OrderID)
			} else {
				left += 1
			}
		}
		return left
	}
	return len(nonTraded)
}

func (s *Strategy) trailingCheck(price float64, direction string) bool {
	return false
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

func (s *Strategy) initTickerFunctions() {
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

func (s *Strategy) Run(ctx context.Context, orderExecutor bbgo.OrderExecutor, session *bbgo.ExchangeSession) error {
	instanceID := s.InstanceID()
	if s.Position == nil {
		s.Position = types.NewPositionFromMarket(s.Market)
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
	s.GeneralOrderExecutor.TradeCollector().OnPositionUpdate(func(p *types.Position) {

		fmt.Println("wwwwww:", s)
		bbgo.Sync(s)
	})
	s.GeneralOrderExecutor.Bind()

	s.orderPendingCounter = make(map[uint64]int)
	s.minutesCounter = 0
	s.holdingCounter = 0

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
	s.GeneralOrderExecutor.TradeCollector().OnTrade(func(trade types.Trade, _profit, _netProfit fixedpoint.Value) {
		price := trade.Price.Float64()
		if s.buyPrice > 0 {
			profit.Update(modify(price / s.buyPrice))
			cumProfit.Update(s.CalcAssetValue(trade.Price).Float64())
		} else if s.sellPrice > 0 {
			profit.Update(modify(s.sellPrice / price))
			cumProfit.Update(s.CalcAssetValue(trade.Price).Float64())
		}
		if s.Position.IsDust(trade.Price) {
			s.buyPrice = 0
			s.sellPrice = 0
			s.highestPrice = 0
			s.lowestPrice = 0
		} else if s.Position.IsLong() {
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
	s.initTickerFunctions()

	startTime := s.Environment.StartTime()
	s.TradeStats.SetIntervalProfitCollector(types.NewIntervalProfitCollector(types.Interval1d, startTime))
	s.TradeStats.SetIntervalProfitCollector(types.NewIntervalProfitCollector(types.Interval1w, startTime))

	// event trigger order: s.Interval => Interval1m
	store, ok := session.MarketDataStore(s.Symbol)

	if !ok {
		panic("cannot get 1m history")
	}
	if err := s.initIndicators(store); err != nil {
		log.WithError(err).Errorf("initIndicator failed")
		return nil
	}
	s.change.Push(HOLD)
	s.InitDrawCommands(store, &profit, &cumProfit)
	store.OnKLineClosed(func(kline types.KLine) {
		s.minutesCounter = int(kline.StartTime.Time().Add(kline.Interval.Duration()).Sub(s.startTime).Minutes())
		if kline.Interval == types.Interval1m {
			s.klineHandler1m(ctx, kline)
		}
		if kline.Interval == s.Interval {
			s.klineHandler(ctx, kline)
		}
	})

	var buy = true

	store.OnKLine(func(kline types.KLine) {

		fmt.Println(s.buyPrice)
		if s.buyPrice > 0 {
			fmt.Println("买了")
			_ = s.ClosePosition(ctx, fixedpoint.One)
			return
		}
		if buy {
			return
		}
		source := s.GetSource(&kline)

		balances := s.GeneralOrderExecutor.Session().GetAccount().Balances()

		quoteBalance, ok := balances[s.Market.QuoteCurrency]
		if !ok {
			log.Errorf("unable to get quoteCurrency")
			return
		}

		fmt.Println("quoteBalance.........", quoteBalance, s.Market.QuoteCurrency)

		quantity := quoteBalance.Available.Div(source)

		param := map[string]interface{}{}
		//param["instId"] = "ETH-USDT-SWAP"
		//param["tdMode"] = "cash"
		//param["ccy"] = "USDT"
		createdOrders, err := s.GeneralOrderExecutor.SubmitOrders(ctx, types.SubmitOrder{
			Symbol: s.Symbol,
			Side:   types.SideTypeBuy,
			Type:   types.OrderTypeLimit,
			Price:  source,

			Quantity: quantity,
			Tag:      "long",
			Params:   param,
		})
		if err != nil {
			log.WithError(err).Errorf("cannot place buy order")
			log.Errorf("%v %v %v", quoteBalance, source, kline)
			return
		}
		buy = true
		fmt.Println(createdOrders)
		//param := map[string]interface{}{}
		//param["instId"] = "ETH-USDT-SWAP"
		//param["tdMode"] = "cross"
		//param["ccy"] = "USDT"
		//fmt.Println(kline)
		//balances := s.GeneralOrderExecutor.Session().GetAccount().Balances()
		//quoteBalance, ok := balances[s.Market.QuoteCurrency]
		//if !ok {
		//	log.Errorf("unable to get quoteCurrency")
		//	return
		//}
		//source := s.GetSource(&kline)
		//
		//quantity := quoteBalance.Available.Div(source)
		//
		//fmt.Println("ni ma quantity", quoteBalance.Available, source, quantity)
		//createdOrders, err := s.GeneralOrderExecutor.SubmitOrders(ctx, types.SubmitOrder{
		//	Symbol:   s.Symbol,
		//	Side:     types.SideTypeBuy,
		//	Type:     types.OrderTypeLimit,
		//	Price:    kline.Low,
		//	Quantity: quantity,
		//	Tag:      "short",
		//	Params:   param,
		//})
		//fmt.Println(createdOrders, err)
	})

	bbgo.OnShutdown(func(ctx context.Context, wg *sync.WaitGroup) {
		var buffer bytes.Buffer
		for _, daypnl := range s.TradeStats.IntervalProfits[types.Interval1d].GetNonProfitableIntervals() {
			fmt.Fprintf(&buffer, "%s\n", daypnl)
		}
		fmt.Fprintln(&buffer, s.TradeStats.BriefString())
		os.Stdout.Write(buffer.Bytes())
		if s.DrawGraph {
			s.Draw(store, &profit, &cumProfit)
		}
		wg.Done()
	})
	return nil
}

func (s *Strategy) CalcAssetValue(price fixedpoint.Value) fixedpoint.Value {
	balances := s.Session.GetAccount().Balances()
	return balances[s.Market.BaseCurrency].Total().Mul(price).Add(balances[s.Market.QuoteCurrency].Total())
}

func (s *Strategy) klineHandler1m(ctx context.Context, kline types.KLine) {
	//if s.Status != types.StrategyStatusRunning {
	//	return
	//}
	//
	//stoploss := s.Stoploss.Float64()
	//price := s.getLastPrice()
	//pricef := price.Float64()
	//
	////todo atr止损倍数
	//atr := s.atr.Last() * 2
	//
	//numPending := s.smartCancel(ctx, pricef)
	//if numPending > 0 {
	//	log.Infof("pending orders: %d, exit", numPending)
	//	return
	//}
	//lowf := math.Min(kline.Low.Float64(), pricef)
	//highf := math.Max(kline.High.Float64(), pricef)
	//if s.lowestPrice > 0 && lowf < s.lowestPrice {
	//	s.lowestPrice = lowf
	//}
	//if s.highestPrice > 0 && highf > s.highestPrice {
	//	s.highestPrice = highf
	//}
	//exitShortCondition := s.sellPrice > 0 && (s.sellPrice*(1.+stoploss) <= highf || s.sellPrice+atr <= highf || s.trailingCheck(highf, "short"))
	//exitLongCondition := s.buyPrice > 0 && (s.buyPrice*(1.-stoploss) >= lowf || s.buyPrice-atr >= lowf ||
	//	s.trailingCheck(lowf, "long"))
	//fmt.Println(s.sellPrice > 0, s.buyPrice*(1.-stoploss) >= lowf, s.buyPrice-atr >= lowf, s.trailingCheck(lowf, "long"))
	//if exitShortCondition || exitLongCondition {
	//	_ = s.ClosePosition(ctx, fixedpoint.One)
	//}
}
func (s *Strategy) klineHandler(ctx context.Context, kline types.KLine) {

	if s.buyPrice > 0 {
		fmt.Println("买了")
		_ = s.ClosePosition(ctx, fixedpoint.One)
		return
	}
	source := s.GetSource(&kline)

	balances := s.GeneralOrderExecutor.Session().GetAccount().Balances()
	quoteBalance, ok := balances[s.Market.QuoteCurrency]
	if !ok {
		log.Errorf("unable to get quoteCurrency")
		return
	}
	if s.Market.IsDustQuantity(
		quoteBalance.Available.Div(source), source) {
		return
	}
	quantity := quoteBalance.Available.Div(source)

	param := map[string]interface{}{}
	//param["instId"] = "ETH-USDT-SWAP"
	//param["tdMode"] = "cash"
	//param["ccy"] = "USDT"
	createdOrders, err := s.GeneralOrderExecutor.SubmitOrders(ctx, types.SubmitOrder{
		Symbol:   s.Symbol,
		Side:     types.SideTypeBuy,
		Type:     types.OrderTypeLimit,
		Price:    source,
		Quantity: quantity,
		Tag:      "long",
		Params:   param,
	})
	if err != nil {
		log.WithError(err).Errorf("cannot place buy order")
		log.Errorf("%v %v %v", quoteBalance, source, kline)
		return
	}
	fmt.Println(createdOrders)

}
func (s *Strategy) klineHandler2(ctx context.Context, kline types.KLine) {
	s.heikinAshi.Update(kline)
	source := s.GetSource(&kline)
	sourcef := source.Float64()
	s.priceLines.Update(sourcef)
	if s.UseHeikinAshi {
		//source := s.GetSource(s.heikinAshi.Last())
		//sourcef := source.Float64()
		//s.ewo.Update(sourcef)
	} else {
		//s.ewo.Update(sourcef)

	}
	if s.UseHeikinAshi {
		source := s.GetSource(s.heikinAshi.Last())
		sourcef := source.Float64()

		s.rsima.Update(sourcef)
	} else {
		//s.ewo.Update(sourcef)
		s.rsima.Update(sourcef)
	}

	//getKline := func(window types.KLineWindow) float64 {
	//	if s.UseHeikinAshi {
	//		return s.heikinAshi.Last().Close.Float64()
	//	}
	//	return window.Close().Last()
	//}

	s.cci.PushK(kline)
	s.alma.Update(sourcef)
	s.macd.PushK(kline)
	s.stoch.PushK(kline)
	s.emaFast.PushK(kline)

	s.emaSlow.PushK(kline)
	s.dema.PushK(kline)
	s.ma.PushK(kline)
	s.hull.PushK(kline)
	s.atr.PushK(kline)
	s.atr.PushK(kline)
	s.atr2.PushK(kline)
	s.dmi.PushK(kline)
	s.rsi.Update(kline.Volume.Float64())
	s.hma.Update(s.rsi.Last())

	//s.rsima.Update(sourcef)

	if s.Status != types.StrategyStatusRunning {
		return
	}

	//stoploss := s.Stoploss.Float64()
	price := s.getLastPrice()
	pricef := price.Float64()
	//lowf := math.Min(kline.Low.Float64(), pricef)
	//highf := math.Min(kline.High.Float64(), pricef)

	s.smartCancel(ctx, pricef)

	//atr := s.atr.Last()
	//ewo := types.Array(s.ewo, 4)
	//if len(ewo) < 4 {
	//	return
	//}
	//bull := kline.Close.Compare(kline.Open) > 0
	//bear := kline.Close.Compare(kline.Open) < 0

	balances := s.GeneralOrderExecutor.Session().GetAccount().Balances()
	bbgo.Notify("source: %.4f, price: %.4f lowest: %.4f highest: %.4f sp %.4f bp %.4f", sourcef, pricef, s.lowestPrice, s.highestPrice, s.sellPrice, s.buyPrice)
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

	maPrice := 0.0
	switch s.MaType {
	case "ema":
		maPrice = s.emaSlow.Last()
	case "sma":
		maPrice = s.ma.Last()
	case "dema":
		maPrice = s.dema.Last()
	case "alma":
		maPrice = s.alma.Last()
	case "hull":
		maPrice = s.hull.Last()

	}

	aboveEma := kline.Close.Compare(fixedpoint.NewFromFloat(maPrice)) > 0
	long := (adx > 20 && dip > 32 && math.Abs(dip-dim) > 5) // && atrdiff && volumeBreak //&& aboveEma // && volumeBreak
	sig := s.change.Last()
	if long {
		sig = BUY
	}
	short := (adx < 20 && dim > 32 && math.Abs(dip-dim) > 5) // && atrdiff && volumeBreak //&& !aboveEma //&& !aboveEma  && volumeBreak
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
	longCondition := changed && s.change.Last() == BUY

	//shortCondition := s.rsi.Last() < 50 && s.emaFast.Last() > s.emaSlow.Last() && redBar && side == types.SideTypeSell
	shortCondition := changed && s.change.Last() == SELL

	//emt := (s.dmi.ADX.Last() < 20 && s.dmi.ADX.Index(1) > 20)
	emt := !aboveEma
	// (s.dmi.ADX.Last() < 20 && s.dmi.ADX.Index(1) > 20)
	//endLongTrade    = longex  or (changed and signal==SELL) or (signal==BUY  and hp_counter==holding_p and not changed)
	//endShortTrade   = shortex or (changed and signal==BUY)  or (signal==SELL and hp_counter==holding_p and not changed)

	closeLong := (changed && s.change.Last() == SELL) || (s.change.Last() == BUY && s.holdingCounter == holdingMax && !changed) || types.CrossUnder(s.dmi.GetDIPlus(), s.dmi.GetDIMinus()).Last()

	closeShort := (changed && s.change.Last() == BUY) || (s.change.Last() == SELL && s.holdingCounter == holdingMax && !changed) || types.CrossOver(s.dmi.GetDIPlus(), s.dmi.GetDIMinus()).Last()

	exitLongCondition := s.buyPrice > 0 && !longCondition || closeLong //|| s.buyPrice-atr >= lowf || kline.Close.Float64() > s.buyPrice+1.5*atr //|| s.trailingCheck(lowf, "long")

	exitShortCondition := s.sellPrice > 0 && !shortCondition || closeShort //|| s.sellPrice+atr <= highf || kline.Close.Float64() < s.sellPrice-1.5*atr //|| s.trailingCheck(highf, "short")
	fmt.Printf("time%s,close%.4f,dip:%.4f,dim:%.4f,adx:%.4f\n", kline.EndTime, kline.Close.Float64(), s.dmi.GetDIPlus().Last(), s.dmi.GetDIMinus().Last(), s.dmi.GetADX().Last())
	fmt.Printf("ema:%.4f,hma:%.4f \n", s.emaSlow.Last(), s.hma.Last())
	fmt.Printf("hma:%.4f,rsi:%.4f,adx %.4f \n", s.hma.Last(), s.rsi.Last(), s.dmi.ADX.Last())
	fmt.Printf("atr1:%.4f,atr2:%.4f \n", s.atr.Last(), s.atr2.Last())
	fmt.Printf("changed:%t,changlast:%s,emt:%t,hoding:%d \n", changed, s.change.Last(), emt, s.holdingCounter)
	fmt.Printf("atrdiff:%t,volumeBreak:%t,aboveEma:%t, \n", atrdiff, volumeBreak, aboveEma)
	fmt.Printf("longCondition:%t,shortCondition:%t, exitLongCondition:%t,exitShortCondition:%t \n", longCondition, shortCondition, exitLongCondition, exitShortCondition)

	//fmt.Println(s.sellPrice > 0, s.buyPrice*(1.-stoploss) >= lowf, s.buyPrice-atr >= lowf, s.trailingCheck(lowf, "long"))

	if exitShortCondition || exitLongCondition {
		if err := s.GeneralOrderExecutor.GracefulCancel(ctx); err != nil {
			log.WithError(err).Errorf("cannot cancel orders")
			return
		}
		s.ClosePosition(ctx, fixedpoint.One)
	}
	if longCondition {
		if err := s.GeneralOrderExecutor.GracefulCancel(ctx); err != nil {
			log.WithError(err).Errorf("cannot cancel orders")
			return
		}
		if source.Compare(price) > 0 {
			source = price
			sourcef = source.Float64()
		}
		balances := s.GeneralOrderExecutor.Session().GetAccount().Balances()
		quoteBalance, ok := balances[s.Market.QuoteCurrency]
		if !ok {
			log.Errorf("unable to get quoteCurrency")
			return
		}
		if s.Market.IsDustQuantity(
			quoteBalance.Available.Div(source), source) {
			return
		}
		quantity := quoteBalance.Available.Div(source)

		param := map[string]interface{}{}
		param["instId"] = "ETH-USDT-SWAP"
		param["tdMode"] = "cross"
		param["ccy"] = "USDT"
		createdOrders, err := s.GeneralOrderExecutor.SubmitOrders(ctx, types.SubmitOrder{
			Symbol:   s.Symbol,
			Side:     types.SideTypeBuy,
			Type:     types.OrderTypeLimit,
			Price:    source,
			Quantity: quantity,
			Tag:      "long",
			Params:   param,
		})
		if err != nil {
			log.WithError(err).Errorf("cannot place buy order")
			log.Errorf("%v %v %v", quoteBalance, source, kline)
			return
		}
		s.orderPendingCounter[createdOrders[0].OrderID] = s.minutesCounter
		return
	}
	if shortCondition {
		if err := s.GeneralOrderExecutor.GracefulCancel(ctx); err != nil {
			log.WithError(err).Errorf("cannot cancel orders")
			return
		}
		if source.Compare(price) < 0 {
			source = price
			sourcef = price.Float64()
		}
		balances := s.GeneralOrderExecutor.Session().GetAccount().Balances()
		baseBalance, ok := balances[s.Market.BaseCurrency]
		if !ok {
			log.Errorf("unable to get baseCurrency")
			return
		}
		if s.Market.IsDustQuantity(baseBalance.Available, source) {
			return
		}
		quantity := baseBalance.Available
		createdOrders, err := s.GeneralOrderExecutor.SubmitOrders(ctx, types.SubmitOrder{
			Symbol:   s.Symbol,
			Side:     types.SideTypeSell,
			Type:     types.OrderTypeLimit,
			Price:    source,
			Quantity: quantity,
			Tag:      "short",
		})
		if err != nil {
			log.WithError(err).Errorf("cannot place sell order")
			return
		}
		s.orderPendingCounter[createdOrders[0].OrderID] = s.minutesCounter
		return
	}
}

//func (s *Strategy) SubmitOrders(ctx context.Context, submitOrders ...types.SubmitOrder) (types.OrderSlice, error) {
//	e:=s.Session.Exchange
//	formattedOrders, err := e.session.FormatOrders(submitOrders)
//	if err != nil {
//		return nil, err
//	}
//
//	createdOrders, errIdx, err := BatchPlaceOrder(ctx, e.session.Exchange, formattedOrders...)
//	if len(errIdx) > 0 {
//		createdOrders2, err2 := BatchRetryPlaceOrder(ctx, e.session.Exchange, errIdx, formattedOrders...)
//		if err2 != nil {
//			err = multierr.Append(err, err2)
//		} else {
//			createdOrders = append(createdOrders, createdOrders2...)
//		}
//	}
//
//	e.orderStore.Add(createdOrders...)
//	e.activeMakerOrders.Add(createdOrders...)
//	e.tradeCollector.Process()
//	return createdOrders, err
//}
