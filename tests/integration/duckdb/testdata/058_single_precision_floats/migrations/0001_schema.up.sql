CREATE TABLE gauges (
  id INTEGER PRIMARY KEY,
  reading FLOAT NOT NULL,
  spare FLOAT4,
  narrow FLOAT(10) NOT NULL,
  wide FLOAT(53) NOT NULL,
  samples FLOAT[] NOT NULL,
  extremes STRUCT(low FLOAT, high DOUBLE)
);
