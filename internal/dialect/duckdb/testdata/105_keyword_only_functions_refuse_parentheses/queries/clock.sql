-- sgl.query(name: TimeCall, command: one)
SELECT current_time() AS refused FROM events;

-- sgl.query(name: TimestampCall, command: one)
SELECT current_timestamp() AS refused FROM events;

-- sgl.query(name: LocalTimeCall, command: one)
SELECT localtime() AS refused FROM events;

-- sgl.query(name: LocalTimestampCall, command: one)
SELECT localtimestamp() AS refused FROM events;
