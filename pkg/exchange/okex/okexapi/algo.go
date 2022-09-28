package okexapi

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
)

//type AlgoOrderResponse struct {
//	Code    string             `json:"sCode"`
//	Message string             `json:"sMsg"`
//	Data    []AlgoOrderDetails `json:"data"`
//}

type AlgoOrderResponse struct {
	AlgoId        string `json:"algoId"`
	ClientOrderID string `json:"clOrdId"`
	Code          string `json:"sCode"`
	Message       string `json:"sMsg"`
}

//go:generate requestgen -type PlaceAlgoOrderRequest
type PlaceAlgoOrderRequest struct {
	client *RestClient

	instrumentID string `param:"instId"`

	// tdMode
	// margin mode: "cross", "isolated"
	// non-margin mode cash
	tradeMode string `param:"tdMode" validValues:"cross,isolated,cash"`

	// A combination of case-sensitive alphanumerics, all numbers, or all letters of up to 32 characters.
	clientOrderID *string `param:"clOrdId"`

	// A combination of case-sensitive alphanumerics, all numbers, or all letters of up to 8 characters.
	tag *string `param:"tag"`

	// "buy" or "sell"
	side SideType `param:"side" validValues:"buy,sell"`

	posSide PosSideType `param:"posSide" validValues:"long,short"`

	orderType OrderType `param:"ordType"`

	quantity string `param:"sz"`
	ccy      string `param:"ccy"`
	tgtCcy   string `param:"tgtCcy"`

	// price
	price      *string `param:"px"`
	reduceOnly bool    `param:"reduceOnly"`

	TpTriggerPx     string `param:"tpTriggerPx"`
	TpOrdPx         string `param:"tpOrdPx"`
	TpTriggerPxType string `param:"tpTriggerPxType" validValues:"last,index,mark"`

	SlTriggerPx string `param:"slTriggerPx"`

	SlOrdPx         string `param:"slOrdPx"`
	SlTriggerPxType string `param:"slTriggerPxType" validValues:"last,index,mark"`
}

func (r *PlaceAlgoOrderRequest) Parameters() map[string]interface{} {
	params, _ := r.GetParameters()
	return params
}

func (r *PlaceAlgoOrderRequest) Do(ctx context.Context) (*AlgoOrderResponse, error) {
	payload := r.Parameters()

	fmt.Println("payload", payload)
	req, err := r.client.newAuthenticatedRequest("POST", "/api/v5/trade/order-algo", nil, payload)
	fmt.Println(req, err)
	if err != nil {
		return nil, err
	}

	response, err := r.client.sendRequest(req)
	if err != nil {
		return nil, err
	}

	var orderResponse struct {
		Code    string              `json:"code"`
		Message string              `json:"msg"`
		Data    []AlgoOrderResponse `json:"data"`
	}
	if err := response.DecodeJSON(&orderResponse); err != nil {
		return nil, err
	}

	if len(orderResponse.Data) == 0 {
		return nil, errors.New("order create error")
	}

	return &orderResponse.Data[0], nil
}

func (c *TradeService) NewPlaceAlgoOrderRequest() *PlaceAlgoOrderRequest {
	return &PlaceAlgoOrderRequest{
		client: c.client,
	}
}
