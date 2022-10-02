package doublecci

import (
	"bytes"
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
	"github.com/sirupsen/logrus"
	"math"
	"os"
	"sync"
	"time"
)

const ID = "scalpma"

var log = logrus.WithField("strategy", ID)
var Two fixedpoint.Value = fixedpoint.NewFromInt(2)
var Three fixedpoint.Value = fixedpoint.NewFromInt(3)
var Four fixedpoint.Value = fixedpoint.NewFromInt(4)
var Delta fixedpoint.Value = fixedpoint.NewFromFloat(0.00001)

func init() {
	bbgo.RegisterStrategy(ID, &Strategy{})
}

type SourceFunc func(*types.KLine) fixedpoint.Value

type ScalpOrder struct {
	Order          types.Order
	TakeProifOrder types.Order
	StopOrder      types.Order
}

type Strategy struct {
	Symbol string `json:"symbol"`

	bbgo.OpenPositionOptions
	bbgo.StrategyController
	bbgo.SourceSelector
	types.Market
	Session *bbgo.ExchangeSession

	Interval       types.Interval   `json:"interval"`
	Stoploss       fixedpoint.Value `json:"stoploss" modifiable:"true"`
	WindowATR      int              `json:"windowATR"`
	WindowMA       int              `json:"windowMA"`
	WindowQuick    int              `json:"windowQuick"`
	WindowSlow     int              `json:"windowSlow"`
	PendingMinutes int              `json:"pendingMinutes" modifiable:"true"`
	UseHeikinAshi  bool             `json:"useHeikinAshi"`

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

	atr *indicator.ATR

	change *indi.Slice
	ma     *indicator.SMA

	priceLines *types.Queue
	orders     *types.Queue

	getLastPrice func() fixedpoint.Value

	// for smart cancel
	orderPendingCounter map[uint64]int
	startTime           time.Time
	minutesCounter      int

	// for position
	buyPrice float64 `persistence:"buy_price"`

	buyTime      time.Time
	sellPrice    float64 `persistence:"sell_price"`
	highestPrice float64 `persistence:"highest_price"`
	lowestPrice  float64 `persistence:"lowest_price"`

	TrailingCallbackRate    []float64          `json:"trailingCallbackRate" modifiable:"true"`
	TrailingActivationRatio []float64          `json:"trailingActivationRatio" modifiable:"true"`
	ExitMethods             bbgo.ExitMethodSet `json:"exits"`

	midPrice   fixedpoint.Value
	lock       sync.RWMutex `ignore:"true"`
	ScalpOrder ScalpOrder
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

// 设置止赢止损
func (s *Strategy) LimitStopPosition(ctx context.Context, tag string) error {

	order := s.Position.NewMarketCloseOrder(fixedpoint.One)
	if order == nil {
		return nil
	}
	order.Tag = tag
	order.TimeInForce = ""
	balances := s.GeneralOrderExecutor.Session().GetAccount().Balances()
	baseBalance := balances[s.Market.BaseCurrency].Available
	price := s.getLastPrice()
	if !s.Session.Futures {
		if order.Side == types.SideTypeBuy {
			quoteAmount := balances[s.Market.QuoteCurrency].Available.Div(price)
			if order.Quantity.Compare(quoteAmount) > 0 {
				order.Quantity = quoteAmount
			}
		} else if order.Side == types.SideTypeSell && order.Quantity.Compare(baseBalance) > 0 {
			order.Quantity = baseBalance
		}
	}

	order.ReduceOnly = true
	order.MarginSideEffect = types.SideEffectTypeAutoRepay

	fmt.Println("--------------------", order.Type)

	for {

		if s.Market.IsDustQuantity(order.Quantity, price) {
			return nil
		}
		if tag == "take" {

			order.Type = types.OrderTypeLimit
			order.Price = fixedpoint.NewFromFloat(s.priceLines.Index(1))

			limit, err := s.GeneralOrderExecutor.SubmitOrders(ctx, *order)
			if err != nil {
				order.Quantity = order.Quantity.Mul(fixedpoint.One.Sub(Delta))
				continue
			}
			if err == nil {
				s.ScalpOrder.StopOrder = limit[0]
			}
			return nil
		} else if tag == "stop" {
			order.Type = types.OrderTypeStopLimit
			order.Price = fixedpoint.NewFromFloat(s.priceLines.Index(0))
			order.StopPrice = fixedpoint.NewFromFloat(s.priceLines.Index(0))
			stop, err := s.GeneralOrderExecutor.SubmitOrders(ctx, *order)
			if err != nil {
				order.Quantity = order.Quantity.Mul(fixedpoint.One.Sub(Delta))
				continue
			}
			if err == nil {
				s.ScalpOrder.StopOrder = stop[0]
			}
			return nil
		}

	}

}
func (s *Strategy) ClosePosition(ctx context.Context, percentage fixedpoint.Value) error {
	//err := s.GeneralOrderExecutor.ClosePosition(ctx, fixedpoint.One)
	//return err
	print(s.buyTime.Add(5*time.Minute), time.Now())
	if !time.Now().After(s.buyTime.Add(5 * time.Minute)) {
		return nil
	}

	order := s.Position.NewMarketCloseOrder(percentage)
	if order == nil {
		return nil
	}
	order.Tag = "close"
	order.TimeInForce = ""
	balances := s.GeneralOrderExecutor.Session().GetAccount().Balances()
	baseBalance := balances[s.Market.BaseCurrency].Available
	price := s.getLastPrice()
	if !s.Session.Futures {
		if order.Side == types.SideTypeBuy {
			quoteAmount := balances[s.Market.QuoteCurrency].Available.Div(price)
			if order.Quantity.Compare(quoteAmount) > 0 {
				order.Quantity = quoteAmount
			}
		} else if order.Side == types.SideTypeSell && order.Quantity.Compare(baseBalance) > 0 {
			order.Quantity = baseBalance
		}
	}

	order.ReduceOnly = true
	order.MarginSideEffect = types.SideEffectTypeAutoRepay
	//if price.Float64() > s.buyPrice {
	//	order.Type = types.OrderTypeTakeProfitLimit
	//
	//}
	//order.Type = types.OrderTypeLimit
	order.Price = price
	fmt.Println("--------------------", order.Type)

	for {

		if s.Market.IsDustQuantity(order.Quantity, price) {
			return nil
		}

		_, err := s.GeneralOrderExecutor.SubmitOrders(ctx, *order)
		if err != nil {
			order.Quantity = order.Quantity.Mul(fixedpoint.One.Sub(Delta))
			continue
		}
		return nil
	}

}

func (s *Strategy) initIndicators(store *bbgo.MarketDataStore) error {
	s.change = &indi.Slice{}
	s.priceLines = types.NewQueue(300)
	//s.cciFast = &indicator.CCI{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowQuick}}
	//s.cciSlow = &indicator.CCI{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowSlow}}

	s.atr = &indicator.ATR{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowATR}}
	s.ma = &indicator.SMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowMA}}

	klines, ok := store.KLinesOfInterval(s.Interval)

	//fmt.Println("klines", klines)
	klineLength := len(*klines)

	if !ok || klineLength == 0 {
		return errors.New("klines not exists")
	}
	tmpK := (*klines)[klineLength-1]
	s.startTime = tmpK.StartTime.Time().Add(tmpK.Interval.Duration())

	for _, kline := range *klines {
		s.AddKline(kline)

	}
	return nil
}
func (s *Strategy) AddKline(kline types.KLine) {
	//source := s.GetSource(&kline).Float64()

	s.atr.PushK(kline)
	s.ma.PushK(kline)
	//s.priceLines.Update(source)
	//
	//sig := s.change.Last()
	//
	//if s.cciFast.Last() < (-200.0) {
	//	sig = "-1"
	//}
	//if s.cciFast.Last() > 200 {
	//	sig = "1"
	//}
	//s.change.Push(sig)

	//fmt.Printf("fast:%.4f,slow:%.4f,chage:%s,ischange:%t", s.cciFast.Last(), s.cciSlow.Last(), s.change.Last(), s.change.Change())
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
	s.ScalpOrder = ScalpOrder{
		Order: types.Order{
			OrderID: 0,
		},
		TakeProifOrder: types.Order{
			OrderID: 0,
		},
		StopOrder: types.Order{
			OrderID: 0,
		},
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
		//fmt.Println("", p)
		bbgo.Sync(s)
	})
	s.GeneralOrderExecutor.Bind()

	s.orderPendingCounter = make(map[uint64]int)
	s.minutesCounter = 0

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
			s.buyTime = time.Now()
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

	s.initOutputCommands()

	// event trigger order: s.Interval => Interval1m
	store, ok := session.MarketDataStore(s.Symbol)
	if !ok {
		panic("cannot get 1m history")
	}
	if err := s.initIndicators(store); err != nil {
		log.WithError(err).Errorf("initIndicator failed")
		return nil
	}
	s.InitDrawCommands(store, &profit, &cumProfit)

	store.OnKLine(func(kline types.KLine) {

		s.klineHandler1m(ctx, kline)

	})

	store.OnKLineClosed(func(kline types.KLine) {
		s.minutesCounter = int(kline.StartTime.Time().Add(kline.Interval.Duration()).Sub(s.startTime).Minutes())
		if kline.Interval == s.Interval {
			s.klineHandler(ctx, kline)
		}
		if kline.Interval == types.Interval1m {
			s.klineHandler1m(ctx, kline)
		}
	})

	//s.Session.UserDataStream.OnOrderUpdate(func(order types.Order) {
	//	if order.OrderID == s.ScalpOrder.Order.OrderID {
	//		s.ScalpOrder.Order = order
	//	}
	//
	//})
	//store.OnKLine(func(kline types.KLine) {
	//
	//	if s.ScalpOrder.Order.OrderID == 0 {
	//		source := s.GetSource(&kline)
	//		//sourcef := source.Float64()
	//		opt := s.OpenPositionOptions
	//		opt.Long = true
	//		opt.Price = kline.Close
	//		opt.LimitOrder = true
	//
	//		opt.Quantity = fixedpoint.NewFromFloat(5).Div(fixedpoint.NewFromFloat(0.1)).Round(0, fixedpoint.Down)
	//		opt.Tags = []string{"long"}
	//		log.Infof("source in long %v %v", source, price)
	//		createdOrders, err := s.GeneralOrderExecutor.OpenPosition(ctx, opt)
	//		if err != nil {
	//			if _, ok := err.(types.ZeroAssetError); ok {
	//				return
	//			}
	//			log.WithError(err).Errorf("cannot place buy order: %v %v", source, kline)
	//			return
	//		}
	//		fmt.Println(createdOrders)
	//		s.ScalpOrder.Order = createdOrders[0]
	//
	//	}
	//	time.Sleep(5 * time.Second)
	//	//fmt.Println(s.Position.IsLong(), s.ScalpOrder.StopOrder.OrderID, s.priceLines.Index(1), s.priceLines.Index(0))
	//	if s.Position.IsLong() && s.ScalpOrder.StopOrder.OrderID == 0 {
	//		s.priceLines.Update(s.buyPrice + 10)
	//		s.priceLines.Update(s.buyPrice - 10)
	//		fmt.Println("s.ScalpOrder.Order.Status", s.ScalpOrder.Order.Status, s.priceLines.Index(1), s.priceLines.Index(0))
	//		if s.ScalpOrder.Order.Status == types.OrderStatusFilled {
	//			if s.ScalpOrder.TakeProifOrder.OrderID == 0 {
	//				s.LimitStopPosition(ctx, "take")
	//			}
	//			if s.ScalpOrder.StopOrder.OrderID == 0 {
	//				s.LimitStopPosition(ctx, "stop")
	//			}
	//
	//		}
	//		//
	//	}
	//
	//	//
	//	//if len(nonTraded) == 0 {
	//	//	if s.buyPrice > 0 {
	//	//		closeOrders := s.ClosePosition(ctx, fixedpoint.One)
	//	//		fmt.Println("close", closeOrders)
	//	//
	//	//	} else {
	//	//
	//	//	}
	//	//}
	//
	//})

	bbgo.OnShutdown(func(ctx context.Context, wg *sync.WaitGroup) {
		var buffer bytes.Buffer
		for _, daypnl := range s.TradeStats.IntervalProfits[types.Interval1d].GetNonProfitableIntervals() {
			fmt.Fprintf(&buffer, "%s\n", daypnl)
		}
		fmt.Fprintln(&buffer, s.TradeStats.BriefString())
		s.Print(&buffer, true, true)
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
	if s.Status != types.StrategyStatusRunning {
		return
	}

	//stoploss := s.Stoploss.Float64()
	price := s.getLastPrice()
	pricef := price.Float64()
	//atr := s.atr.Last()

	//numPending := s.smartCancel(ctx, pricef)
	//if numPending > 0 {
	//	log.Infof("pending orders: %d, exit", numPending)
	//	return
	//}
	lowf := math.Min(kline.Low.Float64(), pricef)
	highf := math.Max(kline.High.Float64(), pricef)
	if s.lowestPrice > 0 && lowf < s.lowestPrice {
		s.lowestPrice = lowf
	}
	if s.highestPrice > 0 && highf > s.highestPrice {
		s.highestPrice = highf
	}

	exitLongCondition := s.buyPrice > 0 && highf > s.priceLines.Index(1)
	//fmt.Println(s.buyPrice > 0, highf, s.priceLines.Index(0), highf > s.priceLines.Index(1), exitLongCondition)
	if exitLongCondition {
		_ = s.ClosePosition(ctx, fixedpoint.One)
	}
}

func (s *Strategy) klineHandler(ctx context.Context, kline types.KLine) {

	source := s.GetSource(&kline)
	sourcef := source.Float64()
	s.AddKline(kline)
	if s.Status != types.StrategyStatusRunning {
		return
	}

	//stoploss := s.Stoploss.Float64()
	price := s.getLastPrice()
	pricef := price.Float64()
	lowf := math.Min(kline.Low.Float64(), pricef)
	highf := math.Min(kline.High.Float64(), pricef)

	s.smartCancel(ctx, pricef)

	atr := s.atr.Last()

	//top3 := s.ma.Last() + atr*4.236
	top2 := s.ma.Last() + atr*2.618
	//top1 := s.ma.Last() + atr*1.618
	//bott1 := s.ma.Last() - atr*1.618
	bott2 := s.ma.Last() - atr*2.618
	bott3 := s.ma.Last() - atr*4.236

	k := 0.005
	//longCondition = ta.cross(low, bott2)
	//shortCondition = ta.cross(high, top2)
	//stop_l = bott3 * (1 - k*0.5)
	//stop_s = top3 * (1 + k)
	//limit_l = top2 * (1 - k)
	//limit_s = bott2
	limitLong := top2 * (1 - k)
	stopLong := bott3 * (1 - k*0.5)

	fmt.Println("top2, bott3,limitLong,stopLong", top2, bott3, limitLong, stopLong)
	s.priceLines.Update(limitLong)
	s.priceLines.Update(stopLong)

	fmt.Printf("sma:%4.f,bott2:%.4f,limit:%.4f,stop:%.4f,atr:%.4f", s.ma.Last(), bott2, s.priceLines.Index(1), s.priceLines.Index(0), atr)
	//s.priceLines.Update(bott3 * (1 - k*0.5))
	//s.priceLines.Update(bott3 * (1 - k*0.5))

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
	//bull := kline.Close.Compare(kline.Open) > 0

	longCondition := kline.Low.Float64() < bott2 && kline.Low.Float64() > bott3 && kline.Close.Compare(kline.Low) > 0

	//shortCondition := !changed && s.change.Last() == "1" && s.cciFast.CrossUnder(s.cciSlow).Last()

	exitLongCondition := s.buyPrice > 0 && !longCondition && (lowf < s.priceLines.Index(0) || highf > s.priceLines.Index(1))

	//exitShortCondition := s.sellPrice > 0 && !shortCondition && s.sellPrice*(1.+stoploss) <= highf || s.sellPrice+atr*6 <= highf || s.trailingCheck(highf, "short")
	if s.Position.IsOpened(kline.Close) {
		bbgo.Notify("%s position is already opened, skip", s.Symbol)
		return
	}
	if exitLongCondition {
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
		}
		opt := s.OpenPositionOptions
		opt.LimitOrder = false
		opt.Long = true
		opt.Price = source
		//
		//opt.Price = kline.Close
		//opt.LimitOrder = false

		opt.Quantity = fixedpoint.NewFromFloat(0.5).Div(fixedpoint.NewFromFloat(0.1)).Round(0, fixedpoint.Down)

		opt.Tags = []string{"long"}
		log.Infof("source in long %v %v", source, price)
		createdOrders, err := s.GeneralOrderExecutor.OpenPosition(ctx, opt)
		if err != nil {
			if _, ok := err.(types.ZeroAssetError); ok {
				return
			}
			log.WithError(err).Errorf("cannot place buy order: %v %v", source, kline)
			return
		}
		if createdOrders != nil {
			s.orderPendingCounter[createdOrders[0].OrderID] = s.minutesCounter
		}
		return
	}
	//if shortCondition && !bull {
	//	if err := s.GeneralOrderExecutor.GracefulCancel(ctx); err != nil {
	//		log.WithError(err).Errorf("cannot cancel orders")
	//		return
	//	}
	//	if source.Compare(price) < 0 {
	//		source = price
	//	}
	//	opt := s.OpenPositionOptions
	//	opt.Short = true
	//	opt.Price = source
	//	opt.Tags = []string{"short"}
	//	log.Infof("source in short %v %v", source, price)
	//	createdOrders, err := s.GeneralOrderExecutor.OpenPosition(ctx, opt)
	//	if err != nil {
	//		if _, ok := err.(types.ZeroAssetError); ok {
	//			return
	//		}
	//		log.WithError(err).Errorf("cannot place sell order: %v %v", source, kline)
	//		return
	//	}
	//	if createdOrders != nil {
	//		s.orderPendingCounter[createdOrders[0].OrderID] = s.minutesCounter
	//	}
	//	return
	//}
}
