package lingo

import (
	"context"
	"fmt"
	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/indicator"
	"github.com/c9s/bbgo/pkg/types"
	"github.com/c9s/bbgo/pkg/util"
	"time"

	"sync"
)

var Two fixedpoint.Value = fixedpoint.NewFromInt(2)
var Three fixedpoint.Value = fixedpoint.NewFromInt(3)
var Four fixedpoint.Value = fixedpoint.NewFromInt(4)
var Delta fixedpoint.Value = fixedpoint.NewFromFloat(0.00001)

type SourceFunc func(*types.KLine) fixedpoint.Value

// RsiEma -- when price breaks the previous pivot low, we set a trade entry
type RsiEma struct {
	bbgo.OpenPositionOptions
	bbgo.StrategyController
	bbgo.SourceSelector
	types.Market
	Session *bbgo.ExchangeSession

	Symbol string
	types.IntervalWindow

	// Ratio is a number less than 1.0, price * ratio will be the price triggers the short order.
	Ratio fixedpoint.Value `json:"ratio"`

	// BounceRatio is a ratio used for placing the limit order sell price
	// limit sell price = breakLowPrice * (1 + BounceRatio)
	BounceRatio   fixedpoint.Value `json:"bounceRatio"`
	orderExecutor *bbgo.GeneralOrderExecutor
	session       *bbgo.ExchangeSession
	*bbgo.Environment

	*types.Position    `persistence:"position"`
	*types.ProfitStats `persistence:"profit_stats"`
	*types.TradeStats  `persistence:"trade_stats"`
	getLastPrice       func() fixedpoint.Value
	midPrice           fixedpoint.Value
	lock               sync.RWMutex `ignore:"true"`

	// for smart cancel
	orderPendingCounter map[uint64]int
	startTime           time.Time
	minutesCounter      int

	// StrategyController
	WindowRSI int `json:"windowRSI"`
	WindowWMA int `json:"windowWMA"`
	WindowEMA int `json:"windowEMA"`
	rsi       *indicator.RSI
	rsi_2h    *indicator.RSI
	rsi_1D    *indicator.RSI
	rsi_1W    *indicator.RSI
	//rsi_1M    *indicator.RSI

	wma    *indicator.WWMA
	wma_2h *indicator.WWMA
	wma_1D *indicator.WWMA
	wma_1W *indicator.WWMA
	//wma_1M *indicator.WWMA

	ema    *indicator.EWMA
	ema_2h *indicator.EWMA
	ema_1D *indicator.EWMA
	ema_1W *indicator.EWMA
	//ema_1M *indicator.EWMA
}

func (s *RsiEma) Subscribe(session *bbgo.ExchangeSession) {
	session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{Interval: types.Interval1m})

	session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{Interval: s.Interval})

	session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{Interval: types.Interval2h})
	session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{Interval: types.Interval1d})
	session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{Interval: types.Interval1w})
	//session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{Interval: types.Interval1mo})

}

// setupIndicators initializes indicators
func (s *RsiEma) setupIndicators() {
	// K-line store for indicators
	kLineStore, _ := s.session.MarketDataStore(s.Symbol)

	s.rsi = &indicator.RSI{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowRSI}}
	s.rsi_2h = &indicator.RSI{IntervalWindow: types.IntervalWindow{Interval: types.Interval2h, Window: s.WindowRSI}}
	s.rsi_1D = &indicator.RSI{IntervalWindow: types.IntervalWindow{Interval: types.Interval1d, Window: s.WindowRSI}}
	s.rsi_1W = &indicator.RSI{IntervalWindow: types.IntervalWindow{Interval: types.Interval1w, Window: s.WindowRSI}}
	//s.rsi_1M = &indicator.RSI{IntervalWindow: types.IntervalWindow{Interval: types.Interval1mo, Window: s.WindowRSI}}

	s.wma = &indicator.WWMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowWMA}}
	s.wma_2h = &indicator.WWMA{IntervalWindow: types.IntervalWindow{Interval: types.Interval2h, Window: s.WindowWMA}}
	s.wma_1D = &indicator.WWMA{IntervalWindow: types.IntervalWindow{Interval: types.Interval1d, Window: s.WindowWMA}}
	s.wma_1W = &indicator.WWMA{IntervalWindow: types.IntervalWindow{Interval: types.Interval1w, Window: s.WindowWMA}}
	//s.wma_1M = &indicator.WWMA{IntervalWindow: types.IntervalWindow{Interval: types.Interval1mo, Window: s.WindowWMA}}

	s.ema = &indicator.EWMA{IntervalWindow: types.IntervalWindow{Interval: s.Interval, Window: s.WindowEMA}}
	s.ema_2h = &indicator.EWMA{IntervalWindow: types.IntervalWindow{Interval: types.Interval2h, Window: s.WindowEMA}}
	s.ema_1D = &indicator.EWMA{IntervalWindow: types.IntervalWindow{Interval: types.Interval1d, Window: s.WindowEMA}}
	s.ema_1W = &indicator.EWMA{IntervalWindow: types.IntervalWindow{Interval: types.Interval1w, Window: s.WindowEMA}}
	//s.ema_1M = &indicator.EWMA{IntervalWindow: types.IntervalWindow{Interval: types.Interval1mo, Window: s.WindowEMA}}

	if klines, ok := kLineStore.KLinesOfInterval(s.Interval); ok {
		for _, kline := range *klines {
			s.rsi.PushK(kline)
			s.ema.Update(s.rsi.Last())
			s.wma.Update(s.rsi.Last())

		}
	}
	if klines, ok := kLineStore.KLinesOfInterval(types.Interval2h); ok {
		for _, kline := range *klines {
			s.rsi_2h.PushK(kline)
			s.ema_2h.Update(s.rsi_2h.Last())
			s.wma_2h.Update(s.rsi_2h.Last())

		}
	}
	if klines, ok := kLineStore.KLinesOfInterval(types.Interval1d); ok {
		for _, kline := range *klines {
			s.rsi_1D.PushK(kline)
			s.ema_1D.Update(s.rsi_1D.Last())
			s.wma_1D.Update(s.rsi_1D.Last())

		}
	}
	if klines, ok := kLineStore.KLinesOfInterval(types.Interval1w); ok {
		for _, kline := range *klines {
			s.rsi_1W.PushK(kline)
			s.ema_1W.Update(s.rsi_1W.Last())
			s.wma_1W.Update(s.rsi_1W.Last())

		}
	}
	//if klines, ok := kLineStore.KLinesOfInterval(types.Interval1mo); ok {
	//	for _, kline := range *klines {
	//		s.ema_1M.PushK(kline)
	//		s.wma_1M.PushK(kline)
	//		s.rsi_1M.PushK(kline)
	//	}
	//}

}

func (s *RsiEma) UpdateIndicators() {
	symbol := s.Symbol
	s.session.MarketDataStream.OnKLineClosed(types.KLineWith(symbol, s.Interval, func(kline types.KLine) {
		s.rsi.PushK(kline)
		s.ema.Update(s.rsi.Last())
		s.wma.Update(s.rsi.Last())
	}))
	s.session.MarketDataStream.OnKLineClosed(types.KLineWith(symbol, types.Interval2h, func(kline types.KLine) {
		s.rsi_2h.PushK(kline)
		s.ema_2h.Update(s.rsi_2h.Last())
		s.wma_2h.Update(s.rsi_2h.Last())

	}))
	s.session.MarketDataStream.OnKLineClosed(types.KLineWith(symbol, types.Interval1d, func(kline types.KLine) {
		s.rsi_1D.PushK(kline)
		s.ema_1D.Update(s.rsi_1D.Last())
		s.wma_1D.Update(s.rsi_1D.Last())

	}))
	s.session.MarketDataStream.OnKLineClosed(types.KLineWith(symbol, types.Interval1w, func(kline types.KLine) {
		s.rsi_1W.PushK(kline)
		s.ema_1W.Update(s.rsi_1W.Last())
		s.wma_1W.Update(s.rsi_1W.Last())
	}))
}

func (s *RsiEma) CalcAssetValue(price fixedpoint.Value) fixedpoint.Value {
	balances := s.session.GetAccount().Balances()
	return balances[s.Market.BaseCurrency].Total().Mul(price).Add(balances[s.Market.QuoteCurrency].Total())
}

func (s *RsiEma) ClosePosition(ctx context.Context, percentage fixedpoint.Value) error {
	//err := s.GeneralOrderExecutor.ClosePosition(ctx, fixedpoint.One)
	//return err
	order := s.Position.NewMarketCloseOrder(percentage)
	if order == nil {
		return nil
	}
	order.Tag = "close"
	order.TimeInForce = ""
	balances := s.orderExecutor.Session().GetAccount().Balances()
	baseBalance := balances[s.Market.BaseCurrency].Available
	price := s.getLastPrice()
	if !s.session.Futures {
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

		_, err := s.orderExecutor.SubmitOrders(ctx, *order)
		if err != nil {
			order.Quantity = order.Quantity.Mul(fixedpoint.One.Sub(Delta))
			continue
		}
		return nil
	}

}

func (s *RsiEma) initTickerFunctions() {
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

func (s *RsiEma) Bind(session *bbgo.ExchangeSession, orderExecutor *bbgo.GeneralOrderExecutor) {

	s.session = session
	s.orderExecutor = orderExecutor

	// StrategyController
	s.Status = types.StrategyStatusRunning

	s.orderPendingCounter = make(map[uint64]int)
	s.minutesCounter = 0

	position := orderExecutor.Position()
	//symbol := position.Symbol

	// update pivot low data
	session.MarketDataStream.OnStart(func() {

	})
	s.setupIndicators()
	s.initTickerFunctions()
	s.UpdateIndicators()

	session.MarketDataStream.OnKLineClosed(types.KLineWith(s.Symbol, types.Interval1m, func(kline types.KLine) {
		source := s.GetSource(&kline)

		if s.Status != types.StrategyStatusRunning {
			return
		}

		//stoploss := s.Stoploss.Float64()
		price := s.getLastPrice()
		// StrategyController
		if s.Status != types.StrategyStatusRunning {
			return
		}

		bbgo.Notify("%s RsiEma signal detected, closed price %f < breakPrice %f", kline.Symbol, kline)

		if position.IsOpened(kline.Close) {
			bbgo.Notify("%s position is already opened, skip", s.Symbol)
			return
		}

		ctx := context.Background()

		bull := kline.Close.Compare(kline.Open) > 0

		balances := s.orderExecutor.Session().GetAccount().Balances()
		//bbgo.Notify("source: %.4f, price: %.4f lowest: %.4f highest: %.4f sp %.4f bp %.4f", sourcef, pricef, s.lowestPrice, s.highestPrice, s.sellPrice, s.buyPrice)
		bbgo.Notify("balances: [Total] %v %s [Base] %s(%v %s) [Quote] %s",
			s.CalcAssetValue(price),
			s.Market.QuoteCurrency,
			balances[s.Market.BaseCurrency].String(),
			balances[s.Market.BaseCurrency].Total().Mul(price),
			s.Market.QuoteCurrency,
			balances[s.Market.QuoteCurrency].String(),
		)
		fmt.Println("s.rsi.Last(), s.wma.Last()", s.rsi.Last(), s.wma.Last())
		fmt.Println("s.rsi_2h.Last() > s.wma_2h.Last()", s.rsi_2h.Last(), s.wma_2h.Last())
		fmt.Println("s.rsi_1W.Last() > s.wma_1W.Last()", s.rsi_1W.Last(), s.wma_1W.Last())
		fmt.Println("s.ema_1W.Last() > s.wma_1W.Last()", s.ema_1W.Last(), s.wma_1W.Last())
		fmt.Println("s.rsi_1W.Last() > s.ema_1W.Last()", s.rsi_1W.Last(), s.ema_1W.Last())
		fmt.Println(s.rsi.Last() > s.wma.Last(), s.rsi.Last() > 50, s.rsi_2h.Last()-s.ema_2h.Last(), s.rsi_2h.Last()-s.ema_2h.Last() >= 1.5 && (s.rsi.Last() > 50 && s.rsi.Index(1) < 50))
		longCondition :=
			s.rsi.Last() > s.wma.Last() &&
				s.rsi_2h.Last() > s.wma_2h.Last() &&
				s.rsi_1W.Last() > s.wma_1W.Last() &&
				s.ema_1W.Last() > s.wma_1W.Last() &&
				s.rsi_1W.Last() > s.ema_1W.Last() &&
				(s.rsi.Last() > s.wma.Last() || s.rsi.Last() > 50 || s.rsi_2h.Last()-s.ema_2h.Last() >= 1.5 && (s.rsi.Last() > 50 && s.rsi.Index(1) < 50))

		//shortCondition :=

		exitLongCondition := position.IsLong() && !longCondition && s.rsi_2h.Last() < s.ema_2h.Last()
		//exitShortCondition := s.sellPrice > 0 && !shortCondition && s.sellPrice*(1.+stoploss) <= highf || s.sellPrice+atr <= highf || s.trailingCheck(highf, "short")

		if exitLongCondition {
			if err := s.orderExecutor.GracefulCancel(ctx); err != nil {
				log.WithError(err).Errorf("cannot cancel orders")
				return
			}
			s.ClosePosition(ctx, fixedpoint.One)
		}
		if longCondition && bull {
			if err := s.orderExecutor.GracefulCancel(ctx); err != nil {
				log.WithError(err).Errorf("cannot cancel orders")
				return
			}
			if source.Compare(price) > 0 {
				source = price
			}
			opt := s.OpenPositionOptions
			opt.Long = true
			opt.Price = source
			opt.Tags = []string{"long"}
			log.Infof("source in long %v %v", source, price)
			createdOrders, err := s.orderExecutor.OpenPosition(ctx, opt)
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

	}))
}
