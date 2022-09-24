adx 策略解读

1、
交易量过滤
volumeBreak(thres) =>
    rsivol   = ta.rsi(volume, 14)
    osc      = ta.hma(rsivol, 10)
    osc > thres

2、atr 过滤

volatilityBreak(volmin, volmax) => ta.atr(volmin) > ta.atr(volmax)


3、均线过滤
价格之上做多，之下做空
filterval   = ta.ema(dataset, filterlag)

4、ADx过滤

[_, _, adx] = ta.dmi(lag, lag)

5 进出信号

signal :=  adx>trendlevel and dataset>filterval and filter ? BUY : adx>trendlevel and dataset<filterval and filter  ? SELL : nz(signal[1])

6 止赢止损进出场

changed = ta.change(signal)

hp_counter := changed ? 0 : hp_counter + 1

if extmethod=='Index'
longex  := ta.crossunder(adx, trendlevel) and (exittype != 'Short')
shortex := ta.crossunder(adx, trendlevel) and (exittype != 'Long')

if extmethod =='Trend'
longex  := ta.crossunder(close, filterval) and (exittype != 'Short')
shortex := ta.crossunder(close, filterval) and (exittype != 'Long')

startLongTrade  = changed and signal==BUY
startShortTrade = changed and signal==SELL
endLongTrade    = longex  or (changed and signal==SELL) or (signal==BUY  and hp_counter==holding_p and not changed)
endShortTrade   = shortex or (changed and signal==BUY)  or (signal==SELL and hp_counter==holding_p and not changed)
