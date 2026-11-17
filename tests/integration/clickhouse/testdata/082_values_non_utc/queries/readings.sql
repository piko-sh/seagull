-- sgl.query(InsertReading, exec)
INSERT INTO readings (id, day, day32, taken_at, taken_at_ms, sensors, gateways, frames, totals, note, position, tags)
VALUES ({id:UInt64}, {day:Date}, {day32:Date32}, {taken_at:DateTime}, {taken_at_ms:DateTime64(3)}, {sensors:Array(UUID)},
    {gateways:Array(IPv4)}, {frames:Array(Array(UInt8))}, {totals:Array(Int128)}, {note:String},
    {position:Tuple(x Float64, y Float64)}, {tags:Map(String, UInt32)});

-- sgl.query(GetReading, one)
SELECT id, toString(day) AS day, toString(day32) AS day32, toUnixTimestamp(taken_at) AS taken_unix,
    toUnixTimestamp64Milli(taken_at_ms) AS taken_milli, length(sensors) AS sensor_count, length(gateways) AS gateway_count,
    length(frames) AS frame_count, length(totals) AS total_count, note, toString(position) AS position, length(tags) AS tag_count
FROM readings WHERE id = {id:UInt64};

-- sgl.query(ReadingsTakenBetween, many)
SELECT id FROM readings WHERE taken_at >= {from:DateTime} AND taken_at < {to:DateTime} ORDER BY id;

-- sgl.query(ReadingsTakenAtMilli, many)
SELECT id FROM readings WHERE taken_at_ms = {at:DateTime64(3)} ORDER BY id;

-- sgl.query(ReadingsOnDay, many)
SELECT id FROM readings WHERE day = {day:Date} AND day32 = {day32:Date32} ORDER BY id;

-- sgl.query(ReadingsWithSensor, many)
SELECT id FROM readings WHERE has(sensors, {sensor:UUID}) ORDER BY id;

-- sgl.query(ReadingsWithGateways, many)
SELECT id FROM readings WHERE gateways = {gateways:Array(IPv4)} ORDER BY id;

-- sgl.query(ReadingsWithFrames, many)
SELECT id FROM readings WHERE frames = {frames:Array(Array(UInt8))} AND totals = {totals:Array(Int128)} ORDER BY id;

-- sgl.query(ReadingsWithTags, many)
SELECT id FROM readings WHERE tags = {tags:Map(String, UInt32)} ORDER BY id;

-- sgl.query(ReadingsWithNote, many)
SELECT id FROM readings WHERE note = {note:String} ORDER BY id;

-- sgl.query(ReadingsIn, many)
-- {ids:UInt64} as sgl.param(ids, kind: slice)
SELECT id FROM readings WHERE id IN ({ids:UInt64}) ORDER BY id;

-- sgl.query(ReadingsNotIn, many)
-- {ids:UInt64} as sgl.param(ids, kind: slice)
SELECT id FROM readings WHERE id NOT IN ({ids:UInt64}) ORDER BY id;

-- sgl.query(InsertNotes, batch)
INSERT INTO notes (id, body) VALUES ({id:UInt64}, concat('note: ', {body:String}));

-- sgl.query(ListNotes, many)
SELECT id, body FROM notes ORDER BY id;
