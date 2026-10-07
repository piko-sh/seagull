-- sgl.query(name: AddMetric, command: exec)
INSERT INTO metrics (id, recorded_on, reading, device) VALUES ($1, $2, $3, $4);

-- sgl.query(name: ListMetrics2026, command: many)
SELECT * FROM metrics_2026 ORDER BY id;

-- sgl.query(name: ListMetrics2027, command: many)
SELECT m.id, m.reading, m.device FROM metrics_2027 AS m ORDER BY m.id;

-- sgl.query(name: MetricNumbers, command: one)
SELECT last_value, is_called FROM metric_numbers;
