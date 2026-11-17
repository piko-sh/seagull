-- sgl.query(InsertMetric, exec)
INSERT INTO metrics (id, sequence, value) VALUES ({id:UInt64}, {sequence:Int64}, {value:Int32});

-- sgl.query(GetMetric, one)
SELECT id, sequence, value FROM metrics WHERE id = {id:UInt64};

-- sgl.query(CountMetrics, one)
SELECT toInt64(count()) AS total FROM metrics;

-- sgl.query(PruneMetricsBelow, exec)
DELETE FROM metrics WHERE id < {cutoff:UInt64};
