-- sgl.query(InsertSighting, exec)
INSERT INTO sightings (id, `legs.colony`, `legs.minutes`, tags, started, departed, note)
VALUES ({id:UInt64}, {colonies:Array(String)}, {minutes:Array(Nullable(UInt16))}, {tags:Array(String)}, {started:Date}, {departed:DateTime}, {note:Nullable(String)});

-- sgl.query(ListLegs, many)
SELECT id, legs.colony, legs.minutes FROM sightings ARRAY JOIN legs ORDER BY id, legs.colony;

-- sgl.query(ListLegTuples, many)
SELECT id, leg.colony AS colony, leg FROM sightings ARRAY JOIN legs AS leg ORDER BY id, colony;

-- sgl.query(ListTagsOrEmpty, many)
SELECT id, tag FROM sightings LEFT ARRAY JOIN tags AS tag ORDER BY id, tag;

-- sgl.query(ListAllTags, many)
SELECT * FROM sightings ARRAY JOIN tags ORDER BY id, tags;

-- sgl.query(MovedDates, one)
SELECT date_add(DAY, 1, started) AS next_day, date_add(HOUR, 2, started) AS next_hour,
    addMilliseconds(departed, 5) AS nudged, started + INTERVAL 30 MINUTE AS half_past
FROM sightings WHERE id = {id:UInt64};
