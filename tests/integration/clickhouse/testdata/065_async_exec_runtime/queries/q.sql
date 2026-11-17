-- sgl.query(InsertEvent, exec)
INSERT INTO events (id, ts, payload) VALUES ({id:UInt64}, {ts:DateTime}, {payload:String});

-- sgl.query(PurgeOld, asyncexec)
ALTER TABLE events DELETE WHERE ts < {cutoff:DateTime};

-- sgl.query(CountAll, one)
SELECT count() AS total FROM events;
