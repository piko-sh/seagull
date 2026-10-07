-- sgl.query(name: InsertPulse, command: exec)
INSERT INTO pulses (ts, source, value) VALUES ($1, $2, $3);

-- sgl.query(name: CountPulses, command: one)
SELECT count(*) FROM pulses;
