package main

import (
	"context"
	"fmt"
	"github.com/c9s/bbgo/pkg/exchange/okex"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
	"os"
	"strings"
	"time"

	"github.com/c9s/bbgo/pkg/exchange/okex/okexapi"
	"github.com/joho/godotenv"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func init() {
	rootCmd.PersistentFlags().String("okex-api-key", "", "okex api key")
	rootCmd.PersistentFlags().String("okex-api-secret", "", "okex api secret")
	rootCmd.PersistentFlags().String("okex-api-passphrase", "", "okex api secret")
	rootCmd.PersistentFlags().String("symbol", "BNBUSDT", "symbol")
}

var rootCmd = &cobra.Command{
	Use:   "okex-book",
	Short: "okex book",

	// SilenceUsage is an option to silence usage when an error occurs.
	SilenceUsage: true,

	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		symbol := viper.GetString("symbol")
		if len(symbol) == 0 {
			return errors.New("empty symbol")
		}

		key, secret, passphrase := viper.GetString("okex-api-key"),
			viper.GetString("okex-api-secret"),
			viper.GetString("okex-api-passphrase")
		if len(key) == 0 || len(secret) == 0 {
			return errors.New("empty key, secret or passphrase")
		}

		client := okexapi.NewClient()
		client.Auth(key, secret, passphrase)
		exchange := okex.New(key, secret, passphrase)
		//
		//instruments, err := client.PublicDataService.NewGetInstrumentsRequest().
		//	InstrumentType("SPOT").Do(ctx)
		//if err != nil {
		//	return err
		//}
		//
		//log.Infof("instruments: %+v", instruments)
		//
		//fundingRate, err := client.PublicDataService.NewGetFundingRate().InstrumentID("BTC-USDT-SWAP").Do(ctx)
		//if err != nil {
		//	return err
		//}
		//log.Infof("funding rate: %+v", fundingRate)
		//
		//log.Infof("ACCOUNT BALANCES:")
		//account, err := client.AccountBalances()
		//if err != nil {
		//	return err
		//}
		//
		//log.Infof("%+v", account)
		//
		//log.Infof("ASSET BALANCES:")
		//assetBalances, err := client.AssetBalances()
		//if err != nil {
		//	return err
		//}
		//
		//for _, balance := range assetBalances {
		//	log.Infof("%T%+v", balance, balance)
		//}
		//
		//log.Infof("ASSET CURRENCIES:")
		//currencies, err := client.AssetCurrencies()
		//if err != nil {
		//	return err
		//}
		//
		//for _, currency := range currencies {
		//	log.Infof("%T%+v", currency, currency)
		//}
		//
		//log.Infof("MARKET TICKERS:")
		//tickers, err := client.MarketTickers(okexapi.InstrumentTypeSpot)
		//if err != nil {
		//	return err
		//}
		//
		//for _, ticker := range tickers {
		//	log.Infof("%T%+v", ticker, ticker)
		//}
		//
		//ticker, err := client.MarketTicker("ETH-USDT")
		//if err != nil {
		//	return err
		//}
		//log.Infof("TICKER:")
		//log.Infof("%T%+v", ticker, ticker)

		log.Infof("PLACING ORDER:")
		placeResponse, err := client.TradeService.NewPlaceOrderRequest().
			InstrumentID("ETH-USDT").
			OrderType(okexapi.OrderTypeMarket).
			Side(okexapi.SideTypeBuy).
			Price("1300.0").
			Quantity("0.5").
			TradeMode("cash").
			Do(ctx)
		if err != nil {
			return err
		}

		log.Infof("place order response: %+v", placeResponse)
		time.Sleep(time.Second)

		price := 1340.0
		quantity := 0.9

		createdTackProfitOrder, err := exchange.SubmitOrder(ctx, types.SubmitOrder{
			Symbol:    "ETHUSDT",
			Market:    market,
			Side:      types.SideTypeSell,
			Type:      types.OrderTypeTakeProfitLimit,
			Price:     fixedpoint.NewFromFloat(price + 20),
			StopPrice: fixedpoint.NewFromFloat(price + 20),
			Quantity:  fixedpoint.NewFromFloat(quantity),

			TimeInForce: "GTC",
			ReduceOnly:  true,
		})
		fmt.Println("take,profit----::\n", createdTackProfitOrder)
		//限价单止损

		createdStopOrder, err := exchange.SubmitOrder(ctx, types.SubmitOrder{
			Symbol:    "ETHUSDT",
			Market:    market,
			Side:      types.SideTypeSell,
			Type:      types.OrderTypeStopLimit,
			Price:     fixedpoint.NewFromFloat(price - 20),
			StopPrice: fixedpoint.NewFromFloat(price - 20),
			Quantity:  fixedpoint.NewFromFloat(quantity),

			TimeInForce: "GTC",
			ReduceOnly:  true,
		})
		fmt.Println("stop-----:", createdStopOrder)

		// cmdutil.WaitForSignal(ctx, syscall.SIGINT, syscall.SIGTERM)
		return nil
	},
}

func main() {
	if _, err := os.Stat(".env.local"); err == nil {
		if err := godotenv.Load(".env.local"); err != nil {
			log.Fatal(err)
		}
	}

	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	if err := viper.BindPFlags(rootCmd.PersistentFlags()); err != nil {
		log.WithError(err).Error("bind pflags error")
	}

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		log.WithError(err).Error("cmd error")
	}
}
