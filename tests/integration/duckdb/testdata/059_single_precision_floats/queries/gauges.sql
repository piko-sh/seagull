-- sgl.query(AddGauge, exec)
-- $7 as sgl.param(low)
-- $8 as sgl.param(high)
INSERT INTO gauges (id, reading, spare, narrow, wide, samples, extremes)
VALUES ($1, $2, $3, $4, $5, $6, {'low': $7::FLOAT, 'high': $8::DOUBLE});

-- sgl.query(SetSamples, exec)
UPDATE gauges SET samples = $1 WHERE id = $2;

-- sgl.query(GaugesSampling, many)
SELECT id FROM gauges WHERE list_has_any(samples, $1::FLOAT[]) ORDER BY id;

-- sgl.query(GetGauge, one)
SELECT id, reading, spare, narrow, wide, samples, extremes, reading + 1 AS bumped, 0.25::FLOAT AS quarter FROM gauges WHERE id = $1;
