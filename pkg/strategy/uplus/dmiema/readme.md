macd:
红空心 代表收盘价小于开盘价
红(空心) 代表收盘相对开盘上涨;

绿色(实心)代表收盘相对开盘下跌
绿色 空心 收盘价大于开盘价


// Calculating
fast_ma = sma_source == "SMA" ? ta.sma(src, fast_length) : ta.ema(src, fast_length)
slow_ma = sma_source == "SMA" ? ta.sma(src, slow_length) : ta.ema(src, slow_length)


dfi = fast_ma - slow_ma
dea = sma_signal == "SMA" ? ta.sma(macd, signal_length) : ta.ema(macd, signal_length)
macd = macd - signal

短期EMA： 短期（例如12日）的收盘价指数移动平均值（Exponential Moving Average）
长期EMA： 长期（例如26日）的收盘价指数移动平均值（Exponential Moving Average）
DIF线：　（Difference）短期EMA和长期EMA的离差值
DEA线：　（Difference Exponential Average）DIF线的M日指数平滑移动平均线
MACD线：　DIF线与DEA线的差


设置
EMA 21 = 绿色
EMA 9 = 黄色
MacD = 5 35 5
RSI = 10
Alma 20 0.8 8


规则做多

：

1. EMA 9 低于 21
2. RSI 高于 50 超卖
3. Macd 实心绿色  收盘小于开盘
4. SL @ ALMA
5. 有条件买入限价单@top wick
6. 1:3 RR

Short:

1. EMA 9 高于 21
2. RSI 低于 50 超买
3. Macd 纯红色
4. SL @ ALMA
5. 有条件限价卖单@bottom wick
6. 1:3 RR