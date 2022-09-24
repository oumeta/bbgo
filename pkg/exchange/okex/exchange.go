package okex

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"

	"github.com/c9s/bbgo/pkg/exchange/okex/okexapi"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

var marketDataLimiter = rate.NewLimiter(rate.Every(time.Second/10), 1)

// OKB is the platform currency of OKEx, pre-allocate static string here
const OKB = "OKB"

var log = logrus.WithFields(logrus.Fields{
	"exchange": "okex",
})

func init() {
	_ = types.Exchange(&Exchange{})
	_ = types.MarginExchange(&Exchange{})
	_ = types.FuturesExchange(&Exchange{})
}

type Exchange struct {
	types.MarginSettings
	types.FuturesSettings
	key, secret, passphrase string

	client *okexapi.RestClient
}

func New(key, secret, passphrase string) *Exchange {
	client := okexapi.NewClient()

	if len(key) > 0 && len(secret) > 0 {
		client.Auth(key, secret, passphrase)
	}

	return &Exchange{
		key: key,
		// pragma: allowlist nextline secret
		secret:     secret,
		passphrase: passphrase,
		client:     client,
	}
}

func (e *Exchange) Name() types.ExchangeName {
	return types.ExchangeOKEx
}

func (e *Exchange) QueryMarkets(ctx context.Context) (types.MarketMap, error) {
	var instruments = []okexapi.Instrument{}

	if e.IsFutures {
		instruments, _ = e.client.PublicDataService.NewGetInstrumentsRequest().
			InstrumentType(okexapi.InstrumentTypeSwap).
			Do(ctx)
		//if err != nil {
		//	return nil, err
		//}
	} else {
		instruments, _ = e.client.PublicDataService.NewGetInstrumentsRequest().
			InstrumentType(okexapi.InstrumentTypeSpot).
			Do(ctx)
		//if err != nil {
		//	return nil, err
		//}
	}

	markets := types.MarketMap{}
	for _, instrument := range instruments {
		symbol := toGlobalSymbol(instrument.InstrumentID)
		market := types.Market{
			Symbol:      symbol,
			LocalSymbol: instrument.InstrumentID,

			QuoteCurrency: instrument.QuoteCurrency,
			BaseCurrency:  instrument.BaseCurrency,

			// convert tick size OKEx to precision
			PricePrecision:  int(-math.Log10(instrument.TickSize.Float64())),
			VolumePrecision: int(-math.Log10(instrument.LotSize.Float64())),

			// TickSize: OKEx's price tick, for BTC-USDT it's "0.1"
			TickSize: instrument.TickSize,

			// Quantity step size, for BTC-USDT, it's "0.00000001"
			StepSize: instrument.LotSize,

			// for BTC-USDT, it's "0.00001"
			MinQuantity: instrument.MinSize,

			// OKEx does not offer minimal notional, use 1 USD here.
			MinNotional: fixedpoint.One,
			MinAmount:   fixedpoint.One,

			CtVal: fixedpoint.MustNewFromString(instrument.ContractValue),
		}

		if e.IsFutures {
			market.QuoteCurrency = instrument.SettleCurrency
			market.BaseCurrency = instrument.ContractValueCurrency
		}

		markets[symbol] = market
	}

	return markets, nil
}

func (e *Exchange) QueryTicker(ctx context.Context, symbol string) (*types.Ticker, error) {
	symbol = toLocalSymbol(symbol)

	marketTicker, err := e.client.MarketTicker(symbol)
	if err != nil {
		return nil, err
	}

	return toGlobalTicker(*marketTicker), nil
}

func (e *Exchange) QueryTickers(ctx context.Context, symbols ...string) (map[string]types.Ticker, error) {
	instrumentType := okexapi.InstrumentTypeSpot
	if e.IsFutures {
		instrumentType = okexapi.InstrumentTypeFutures
	}

	marketTickers, err := e.client.MarketTickers(instrumentType)
	if err != nil {
		return nil, err
	}

	tickers := make(map[string]types.Ticker)
	for _, marketTicker := range marketTickers {
		symbol := toGlobalSymbol(marketTicker.InstrumentID)
		ticker := toGlobalTicker(marketTicker)
		tickers[symbol] = *ticker
	}

	if len(symbols) == 0 {
		return tickers, nil
	}

	selectedTickers := make(map[string]types.Ticker, len(symbols))
	for _, symbol := range symbols {
		if ticker, ok := tickers[symbol]; ok {
			selectedTickers[symbol] = ticker
		}
	}

	return selectedTickers, nil
}

func (e *Exchange) PlatformFeeCurrency() string {
	return OKB
}

func (e *Exchange) QueryAccount(ctx context.Context) (*types.Account, error) {
	accountBalance, err := e.client.AccountBalances()
	if err != nil {
		return nil, err
	}

	var account = types.Account{
		AccountType: "SPOT",
	}

	var balanceMap = toGlobalBalance(accountBalance)
	account.UpdateBalances(balanceMap)
	return &account, nil
}

func (e *Exchange) QueryAccountBalances(ctx context.Context) (types.BalanceMap, error) {
	accountBalances, err := e.client.AccountBalances()
	if err != nil {
		return nil, err
	}

	var balanceMap = toGlobalBalance(accountBalances)
	return balanceMap, nil
}

func (e *Exchange) SubmitOrder(ctx context.Context, order types.SubmitOrder) (*types.Order, error) {
	orderReq := e.client.TradeService.NewPlaceOrderRequest()
	param := order.Params

	orderType, err := toLocalOrderType(order.Type)
	if err != nil {
		return nil, err
	}

	orderReq.InstrumentID(toLocalSymbol(order.Symbol))
	orderReq.Side(toLocalSideType(order.Side))

	if order.Market.Symbol != "" {
		orderReq.Quantity(order.Market.FormatQuantity(order.Quantity))
	} else {
		// TODO report error
		orderReq.Quantity(order.Quantity.FormatString(8))
	}

	// set price field for limit orders
	switch order.Type {
	case types.OrderTypeStopLimit, types.OrderTypeLimit, types.OrderTypeLimitMaker:
		if order.Market.Symbol != "" {
			orderReq.Price(order.Market.FormatPrice(order.Price))
		} else {
			// TODO report error
			orderReq.Price(order.Price.FormatString(8))
		}
	}

	switch order.TimeInForce {
	case "FOK":
		orderReq.OrderType(okexapi.OrderTypeFOK)
	case "IOC":
		orderReq.OrderType(okexapi.OrderTypeIOC)
	default:
		orderReq.OrderType(orderType)
	}

	if order.Params != nil {
		if param["tdMode"] != nil {
			orderReq.TradeMode(param["tdMode"].(string))

		}
		if param["instId"] != nil {
			orderReq.InstrumentID(param["instId"].(string))

		}
		//orderReq.InstrumentID(param["instId"].(string))
		if param["ccy"] != nil {
			orderReq.CCY(param["ccy"].(string))

		}
		if param["tgtCcy"] != nil {
			orderReq.TgtCcy(param["tgtCcy"].(string))

		}
	}

	if e.IsFutures {
		side := okexapi.PosSideTypeBuy
		if order.Side == types.SideTypeBuy {
			side = okexapi.PosSideTypeBuy
		}
		if order.Side == types.SideTypeSell {
			side = okexapi.PosSideTypeSell
		}

		orderReq.PosSide(side)
		orderReq.TradeMode("cross")
		orderReq.TgtCcy("USDT")
		if order.ReduceOnly {

			fmt.Println("close ", order.Quantity.Div(fixedpoint.NewFromFloat(0.1)).Round(0, fixedpoint.Down).String())
			orderReq.ReduceOnly(order.ReduceOnly)
			orderReq.Quantity(order.Quantity.Div(fixedpoint.NewFromFloat(0.1)).Round(0, fixedpoint.Down).String())

		} else {
			orderReq.Quantity(order.Quantity.Div(fixedpoint.NewFromFloat(0.1)).Round(0, fixedpoint.Down).String())

		}

	} else {
		orderReq.TradeMode("cash")
	}

	//orderReq("cash")
	orderHead, err := orderReq.Do(ctx)
	if err != nil {
		return nil, err
	}

	var orderID int64
	if orderHead.OrderID != "" {
		orderID, err = strconv.ParseInt(orderHead.OrderID, 10, 64)

		if err != nil {
			return nil, err
		}
	} else {
		return nil, nil
	}

	return &types.Order{
		SubmitOrder:      order,
		Exchange:         types.ExchangeOKEx,
		OrderID:          uint64(orderID),
		Status:           types.OrderStatusNew,
		ExecutedQuantity: fixedpoint.Zero,
		IsWorking:        true,
		CreationTime:     types.Time(time.Now()),
		UpdateTime:       types.Time(time.Now()),
		IsMargin:         false,
		IsIsolated:       false,
	}, nil

	// TODO: move this to batch place orders interface
	/*
		batchReq := e.client.TradeService.NewBatchPlaceOrderRequest()
		batchReq.Add(reqs...)
		orderHeads, err := batchReq.Do(ctx)
		if err != nil {
			return nil, err
		}

		for idx, orderHead := range orderHeads {
			orderID, err := strconv.ParseInt(orderHead.OrderID, 10, 64)
			if err != nil {
				return createdOrder, err
			}

			submitOrder := order[idx]
			createdOrder = append(createdOrder, types.Order{
				SubmitOrder:      submitOrder,
				Exchange:         types.ExchangeOKEx,
				OrderID:          uint64(orderID),
				Status:           types.OrderStatusNew,
				ExecutedQuantity: fixedpoint.Zero,
				IsWorking:        true,
				CreationTime:     types.Time(time.Now()),
				UpdateTime:       types.Time(time.Now()),
				IsMargin:         false,
				IsIsolated:       false,
			})
		}
	*/
}

func (e *Exchange) QueryOpenOrders(ctx context.Context, symbol string) (orders []types.Order, err error) {
	instrumentID := toLocalSymbol(symbol)
	req := e.client.TradeService.NewGetPendingOrderRequest().InstrumentType(okexapi.InstrumentTypeSpot).InstrumentID(instrumentID)
	orderDetails, err := req.Do(ctx)
	if err != nil {
		return orders, err
	}

	orders, err = toGlobalOrders(orderDetails)
	return orders, err
}

func (e *Exchange) CancelOrders(ctx context.Context, orders ...types.Order) error {
	if len(orders) == 0 {
		return nil
	}

	var reqs []*okexapi.CancelOrderRequest
	for _, order := range orders {
		if len(order.Symbol) == 0 {
			return errors.New("symbol is required for canceling an okex order")
		}

		req := e.client.TradeService.NewCancelOrderRequest()
		req.InstrumentID(toLocalSymbol(order.Symbol))
		req.OrderID(strconv.FormatUint(order.OrderID, 10))
		if len(order.ClientOrderID) > 0 {
			req.ClientOrderID(order.ClientOrderID)
		}
		reqs = append(reqs, req)
	}

	batchReq := e.client.TradeService.NewBatchCancelOrderRequest()
	batchReq.Add(reqs...)
	_, err := batchReq.Do(ctx)
	return err
}

func (e *Exchange) NewStream() types.Stream {
	return NewStream(e.client)
}

func (e *Exchange) QueryKLines(ctx context.Context, symbol string, interval types.Interval, options types.KLineQueryOptions) ([]types.KLine, error) {
	if err := marketDataLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	intervalParam := toLocalInterval(interval.String())

	req := e.client.MarketDataService.NewCandlesticksRequest(toLocalSymbol(symbol))
	req.Bar(intervalParam)

	if options.StartTime != nil {
		req.After(options.StartTime.Unix())
	}

	if options.EndTime != nil {
		req.Before(options.EndTime.Unix())
	}

	candles, err := req.Do(ctx)
	if err != nil {
		return nil, err
	}

	var klines []types.KLine
	for _, candle := range candles {
		klines = append(klines, types.KLine{
			Exchange:    types.ExchangeOKEx,
			Symbol:      symbol,
			Interval:    interval,
			Open:        candle.Open,
			High:        candle.High,
			Low:         candle.Low,
			Close:       candle.Close,
			LastTradeID: 0,
			Closed:      true,
			Volume:      candle.Volume,
			QuoteVolume: candle.VolumeInCurrency,
			StartTime:   types.Time(candle.Time),
			EndTime:     types.Time(candle.Time.Add(interval.Duration() - time.Millisecond)),
		})
	}
	klines = types.SortKLinesAscending(klines)

	return klines, nil

}
