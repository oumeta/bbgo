package newdmi

import (
	"context"
	"fmt"
	"github.com/davecgh/go-spew/spew"
	"os"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/dynamic"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

const ID = "newdmi"

var one = fixedpoint.One

var log = logrus.WithField("strategy", ID)

func init() {
	bbgo.RegisterStrategy(ID, &Strategy{})
}

type Strategy struct {
	Environment *bbgo.Environment
	Symbol      string `json:"symbol"`
	Market      types.Market

	// pivot interval and window
	types.IntervalWindow

	Leverage fixedpoint.Value `json:"leverage"`
	Quantity fixedpoint.Value `json:"quantity"`

	// persistence fields

	Position    *types.Position    `persistence:"position"`
	ProfitStats *types.ProfitStats `persistence:"profit_stats"`
	TradeStats  *types.TradeStats  `persistence:"trade_stats"`

	// BreakLow is one of the entry method
	DmiBreak *DmiBreak `json:"dmiBreak"`

	DmiCross *DmiCross `json:"dmiCross"`

	DmiMacd *DmiMacd `json:"dmiMacd"`

	ExitMethods bbgo.ExitMethodSet `json:"exits"`

	session       *bbgo.ExchangeSession
	orderExecutor *bbgo.GeneralOrderExecutor

	// StrategyController
	bbgo.StrategyController
}

func (s *Strategy) ID() string {
	return ID
}

func (s *Strategy) InstanceID() string {
	return fmt.Sprintf("%s:%s", ID, s.Symbol)
}

func (s *Strategy) Subscribe(session *bbgo.ExchangeSession) {
	session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{Interval: s.Interval})
	session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{Interval: types.Interval1m})

	if s.DmiBreak != nil {
		dynamic.InheritStructValues(s.DmiBreak, s)
		s.DmiBreak.Subscribe(session)
	}
	if s.DmiCross != nil {
		dynamic.InheritStructValues(s.DmiCross, s)
		s.DmiCross.Subscribe(session)
	}
	if s.DmiMacd != nil {
		dynamic.InheritStructValues(s.DmiMacd, s)
		s.DmiMacd.Subscribe(session)
	}

	if !bbgo.IsBackTesting {
		session.Subscribe(types.MarketTradeChannel, s.Symbol, types.SubscribeOptions{})
	}

	s.ExitMethods.SetAndSubscribe(session, s)
}

func (s *Strategy) CurrentPosition() *types.Position {
	return s.Position
}

func (s *Strategy) ClosePosition(ctx context.Context, percentage fixedpoint.Value) error {
	return s.orderExecutor.ClosePosition(ctx, percentage)
}

func (s *Strategy) handleOrderUpdate(order types.Order) {
	fmt.Println(order)
}

func (s *Strategy) Run(ctx context.Context, orderExecutor bbgo.OrderExecutor, session *bbgo.ExchangeSession) error {
	var instanceID = s.InstanceID()
	fmt.Println(s.Market)
	if s.Position == nil {
		s.Position = types.NewPositionFromMarket(s.Market)
	}

	if s.ProfitStats == nil {
		s.ProfitStats = types.NewProfitStats(s.Market)
	}

	if s.TradeStats == nil {
		s.TradeStats = types.NewTradeStats(s.Symbol)
	}

	if s.Leverage.IsZero() {
		// the default leverage is 3x
		s.Leverage = fixedpoint.NewFromInt(3)
	}

	// StrategyController
	s.Status = types.StrategyStatusRunning

	s.OnSuspend(func() {
		// Cancel active orders
		_ = s.orderExecutor.GracefulCancel(ctx)

		if s.DmiBreak != nil {
			s.DmiBreak.Suspend()
		}
		if s.DmiCross != nil {
			s.DmiCross.Suspend()
		}
		if s.DmiMacd != nil {
			s.DmiMacd.Suspend()
		}

	})

	s.OnEmergencyStop(func() {
		// Cancel active orders
		_ = s.orderExecutor.GracefulCancel(ctx)
		// Close 100% position
		_ = s.ClosePosition(ctx, fixedpoint.One)

		if s.DmiBreak != nil {
			s.DmiBreak.EmergencyStop()
		}
		if s.DmiCross != nil {
			s.DmiCross.EmergencyStop()
		}
		if s.DmiMacd != nil {
			s.DmiMacd.EmergencyStop()
		}

	})

	// initial required information
	s.session = session
	s.orderExecutor = bbgo.NewGeneralOrderExecutor(session, s.Symbol, ID, instanceID, s.Position)
	s.orderExecutor.BindEnvironment(s.Environment)
	s.orderExecutor.BindProfitStats(s.ProfitStats)
	s.orderExecutor.BindTradeStats(s.TradeStats)
	s.orderExecutor.TradeCollector().OnPositionUpdate(func(position *types.Position) {
		fmt.Println("nima bi deasdfasdf--------:", position)
		bbgo.Sync(s)
	})
	s.orderExecutor.Bind()

	session.UserDataStream.OnFuturesPositionSnapshot(func(futuresPositions types.FuturesPositionMap) {
		spew.Dump(futuresPositions)
	})
	session.UserDataStream.OnStart(func() {
		fmt.Println("kkkkkkkkk-------")
	})

	s.orderExecutor.TradeCollector().OnPositionUpdate(func(position *types.Position) {
		fmt.Println("aaa-------", position)
	})
	s.orderExecutor.Bind()

	s.orderExecutor.TradeCollector().OnTrade(func(trade types.Trade, profit, netprofit fixedpoint.Value) {
		fmt.Println("bbb-------", trade, profit)

	})

	s.ExitMethods.Bind(session, s.orderExecutor)

	if s.DmiBreak != nil {
		s.DmiBreak.Bind(session, s.orderExecutor)
	}
	if s.DmiCross != nil {
		s.DmiCross.Bind(session, s.orderExecutor)
	}
	if s.DmiMacd != nil {
		s.DmiMacd.Bind(session, s.orderExecutor)
	}

	bbgo.OnShutdown(func(ctx context.Context, wg *sync.WaitGroup) {
		defer wg.Done()

		_, _ = fmt.Fprintln(os.Stderr, s.TradeStats.String())
		_ = s.orderExecutor.GracefulCancel(ctx)
	})

	return nil
}
