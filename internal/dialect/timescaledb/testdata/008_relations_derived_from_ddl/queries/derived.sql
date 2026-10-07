-- sgl.query(name: ListPartitionMetrics, command: many)
SELECT id, recorded_on, reading, device FROM metrics_2026;

-- sgl.query(name: MetricNumber, command: one)
SELECT last_value FROM metric_numbers;

-- sgl.query(name: ListSnapshots, command: many)
SELECT id, taken_at FROM archive.snapshots;
