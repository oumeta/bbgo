// Code generated by "requestgen -type PlaceAlgoOrderRequest"; DO NOT EDIT.

package okexapi

import (
	"encoding/json"
	"fmt"
	"github.com/davecgh/go-spew/spew"
	"net/url"
	"reflect"
	"regexp"
)

func (p *PlaceAlgoOrderRequest) InstrumentID(instrumentID string) *PlaceAlgoOrderRequest {
	p.instrumentID = instrumentID
	return p
}

func (p *PlaceAlgoOrderRequest) TradeMode(tradeMode string) *PlaceAlgoOrderRequest {
	p.tradeMode = tradeMode
	return p
}

func (p *PlaceAlgoOrderRequest) ClientOrderID(clientOrderID string) *PlaceAlgoOrderRequest {
	p.clientOrderID = &clientOrderID
	return p
}

func (p *PlaceAlgoOrderRequest) Tag(tag string) *PlaceAlgoOrderRequest {
	p.tag = &tag
	return p
}

func (p *PlaceAlgoOrderRequest) Side(side SideType) *PlaceAlgoOrderRequest {
	p.side = side
	return p
}

func (p *PlaceAlgoOrderRequest) PosSide(posSide PosSideType) *PlaceAlgoOrderRequest {
	p.posSide = posSide
	return p
}

func (p *PlaceAlgoOrderRequest) OrderType(orderType OrderType) *PlaceAlgoOrderRequest {
	p.orderType = orderType
	return p
}

func (p *PlaceAlgoOrderRequest) Quantity(quantity string) *PlaceAlgoOrderRequest {
	p.quantity = quantity
	return p
}

func (p *PlaceAlgoOrderRequest) Ccy(ccy string) *PlaceAlgoOrderRequest {
	p.ccy = ccy
	return p
}

func (p *PlaceAlgoOrderRequest) TgtCcy(tgtCcy string) *PlaceAlgoOrderRequest {
	p.tgtCcy = tgtCcy
	return p
}

func (p *PlaceAlgoOrderRequest) Price(price string) *PlaceAlgoOrderRequest {
	p.price = &price
	return p
}

func (p *PlaceAlgoOrderRequest) ReduceOnly(reduceOnly bool) *PlaceAlgoOrderRequest {
	p.reduceOnly = reduceOnly
	return p
}

func (p *PlaceAlgoOrderRequest) SetTpTriggerPx(TpTriggerPx string) *PlaceAlgoOrderRequest {
	p.TpTriggerPx = TpTriggerPx
	return p
}

func (p *PlaceAlgoOrderRequest) SetTpOrdPx(TpOrdPx string) *PlaceAlgoOrderRequest {
	p.TpOrdPx = TpOrdPx
	return p
}

func (p *PlaceAlgoOrderRequest) SetTpTriggerPxType(TpTriggerPxType string) *PlaceAlgoOrderRequest {
	p.TpTriggerPxType = TpTriggerPxType
	return p
}

func (p *PlaceAlgoOrderRequest) SetSlTriggerPx(SlTriggerPx string) *PlaceAlgoOrderRequest {
	p.SlTriggerPx = SlTriggerPx
	return p
}

func (p *PlaceAlgoOrderRequest) SetSlOrdPx(SlOrdPx string) *PlaceAlgoOrderRequest {
	p.SlOrdPx = SlOrdPx
	return p
}

func (p *PlaceAlgoOrderRequest) SetSlTriggerPxType(SlTriggerPxType string) *PlaceAlgoOrderRequest {
	p.SlTriggerPxType = SlTriggerPxType
	return p
}

// GetQueryParameters builds and checks the query parameters and returns url.Values
func (p *PlaceAlgoOrderRequest) GetQueryParameters() (url.Values, error) {
	var params = map[string]interface{}{}

	query := url.Values{}
	for _k, _v := range params {
		query.Add(_k, fmt.Sprintf("%v", _v))
	}

	return query, nil
}

// GetParameters builds and checks the parameters and return the result in a map object
func (p *PlaceAlgoOrderRequest) GetParameters() (map[string]interface{}, error) {
	var params = map[string]interface{}{}
	// check instrumentID field -> json key instId
	instrumentID := p.instrumentID

	// assign parameter of instrumentID
	params["instId"] = instrumentID
	// check tradeMode field -> json key tdMode
	tradeMode := p.tradeMode

	// TEMPLATE check-valid-values
	switch tradeMode {
	case "cross", "isolated", "cash":
		params["tdMode"] = tradeMode

	default:
		return nil, fmt.Errorf("tdMode value %v is invalid", tradeMode)

	}

	// assign parameter of tradeMode
	params["tdMode"] = tradeMode
	// check clientOrderID field -> json key clOrdId
	if p.clientOrderID != nil {
		clientOrderID := *p.clientOrderID

		// assign parameter of clientOrderID
		params["clOrdId"] = clientOrderID
	} else {
	}

	// check tag field -> json key tag
	if p.tag != nil {
		tag := *p.tag

		// assign parameter of tag
		params["tag"] = tag
	} else {
	}
	// check side field -> json key side
	side := p.side

	// TEMPLATE check-valid-values
	switch side {
	case "buy", "sell":
		params["side"] = side

	}

	// END TEMPLATE check-valid-values

	// assign parameter of side
	params["side"] = side
	// check posSide field -> json key posSide
	posSide := p.posSide

	// TEMPLATE check-valid-values
	switch posSide {
	case "long", "short":
		params["posSide"] = posSide

	default:
		return nil, fmt.Errorf("posSide value %v is invalid", posSide)

	}
	// END TEMPLATE check-valid-values

	// assign parameter of posSide
	params["posSide"] = posSide
	// check orderType field -> json key ordType
	orderType := p.orderType

	params["ordType"] = orderType

	quantity := p.quantity

	// assign parameter of quantity
	params["sz"] = quantity
	// check ccy field -> json key ccy
	ccy := p.ccy

	// assign parameter of ccy
	params["ccy"] = ccy
	// check tgtCcy field -> json key tgtCcy
	tgtCcy := p.tgtCcy

	// assign parameter of tgtCcy
	params["tgtCcy"] = tgtCcy
	// check price field -> json key px
	if p.price != nil {
		price := *p.price

		// assign parameter of price
		params["px"] = price
	} else {
	}
	// check reduceOnly field -> json key reduceOnly
	reduceOnly := p.reduceOnly

	// assign parameter of reduceOnly
	params["reduceOnly"] = reduceOnly
	// check TpTriggerPx field -> json key tpTriggerPx

	// check TpTriggerPxType field -> json key tpTriggerPxType
	TpTriggerPxType := p.TpTriggerPxType

	// TEMPLATE check-valid-values
	switch TpTriggerPxType {
	case "last", "index", "mark":
		params["tpTriggerPxType"] = TpTriggerPxType
		TpTriggerPx := p.TpTriggerPx

		// assign parameter of TpTriggerPx
		params["tpTriggerPx"] = TpTriggerPx
		// check TpOrdPx field -> json key tpOrdPx
		TpOrdPx := p.TpOrdPx

		// assign parameter of TpOrdPx
		params["tpOrdPx"] = TpOrdPx

	}

	SlTriggerPxType := p.SlTriggerPxType

	// TEMPLATE check-valid-values
	switch SlTriggerPxType {
	case "last", "index", "mark":
		params["slTriggerPxType"] = SlTriggerPxType
		// assign parameter of TpTriggerPxType
		params["tpTriggerPxType"] = TpTriggerPxType
		// check SlTriggerPx field -> json key slTriggerPx
		SlTriggerPx := p.SlTriggerPx

		// assign parameter of SlTriggerPx
		params["slTriggerPx"] = SlTriggerPx
		// check SlOrdPx field -> json key slOrdPx
		SlOrdPx := p.SlOrdPx

		// assign parameter of SlOrdPx
		params["slOrdPx"] = SlOrdPx

	}

	spew.Dump(params)
	return params, nil
}

// GetParametersQuery converts the parameters from GetParameters into the url.Values format
func (p *PlaceAlgoOrderRequest) GetParametersQuery() (url.Values, error) {
	query := url.Values{}

	params, err := p.GetParameters()
	if err != nil {
		return query, err
	}

	for _k, _v := range params {
		if p.isVarSlice(_v) {
			p.iterateSlice(_v, func(it interface{}) {
				query.Add(_k+"[]", fmt.Sprintf("%v", it))
			})
		} else {
			query.Add(_k, fmt.Sprintf("%v", _v))
		}
	}

	return query, nil
}

// GetParametersJSON converts the parameters from GetParameters into the JSON format
func (p *PlaceAlgoOrderRequest) GetParametersJSON() ([]byte, error) {
	params, err := p.GetParameters()
	if err != nil {
		return nil, err
	}

	return json.Marshal(params)
}

// GetSlugParameters builds and checks the slug parameters and return the result in a map object
func (p *PlaceAlgoOrderRequest) GetSlugParameters() (map[string]interface{}, error) {
	var params = map[string]interface{}{}

	return params, nil
}

func (p *PlaceAlgoOrderRequest) applySlugsToUrl(url string, slugs map[string]string) string {
	for _k, _v := range slugs {
		needleRE := regexp.MustCompile(":" + _k + "\\b")
		url = needleRE.ReplaceAllString(url, _v)
	}

	return url
}

func (p *PlaceAlgoOrderRequest) iterateSlice(slice interface{}, _f func(it interface{})) {
	sliceValue := reflect.ValueOf(slice)
	for _i := 0; _i < sliceValue.Len(); _i++ {
		it := sliceValue.Index(_i).Interface()
		_f(it)
	}
}

func (p *PlaceAlgoOrderRequest) isVarSlice(_v interface{}) bool {
	rt := reflect.TypeOf(_v)
	switch rt.Kind() {
	case reflect.Slice:
		return true
	}
	return false
}

func (p *PlaceAlgoOrderRequest) GetSlugsMap() (map[string]string, error) {
	slugs := map[string]string{}
	params, err := p.GetSlugParameters()
	if err != nil {
		return slugs, nil
	}

	for _k, _v := range params {
		slugs[_k] = fmt.Sprintf("%v", _v)
	}

	return slugs, nil
}