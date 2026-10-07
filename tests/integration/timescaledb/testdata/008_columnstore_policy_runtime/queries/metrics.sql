-- sgl.query(name: InsertMetric, command: exec)
INSERT INTO metrics (ts, device, value) VALUES ($1, $2, $3);

-- sgl.query(name: SumByDevice, command: many)
SELECT device, sum(value) AS total
FROM metrics
GROUP BY device
ORDER BY device;
