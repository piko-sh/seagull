-- sgl.query(name: InsertTelemetry, command: exec)
INSERT INTO telemetry (ts, device_id, value) VALUES ($1, $2, $3);

-- sgl.query(name: CountTelemetry, command: one)
SELECT count(*) FROM telemetry;
