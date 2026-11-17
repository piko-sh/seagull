-- sgl.query(name: RoundedPrices, command: many)
SELECT id, round(price, 1) AS rounded, roundBankers(price, 1) AS bankers, floor(price) AS floored,
       ceil(price, 1) AS ceiled, trunc(price) AS truncated, round(latency_ms, -1) AS rough_latency,
       round(ratio, 2) AS rough_ratio, round(discount, 1) AS rough_discount
FROM orders;

-- sgl.query(name: LatencyQuantiles, command: one)
SELECT quantileExact(latency_ms) AS median_latency, quantileExactLow(0.9)(latency_ms) AS low_p90,
       quantileExactHigh(0.9)(price) AS high_p90_price, quantileExactWeighted(latency_ms, id) AS weighted,
       quantileExact(placed_on) AS median_day, quantilesExact(0.5, 0.9)(latency_ms) AS latencies,
       quantile(latency_ms) AS approximate
FROM orders;
