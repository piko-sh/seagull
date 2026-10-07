-- sgl.query(MoorBoat, exec)
INSERT INTO boats (name, berth, crew, route, fees) VALUES ($1, $2, $3, $4, $5);

-- sgl.query(ContactByPhone, execrows)
-- $1 as sgl.param(phone)
UPDATE boats SET contact = $1::VARCHAR WHERE name = $2;

-- sgl.query(ContactByRadio, execrows)
-- $1 as sgl.param(channel)
UPDATE boats SET contact = $1::INTEGER WHERE name = $2;

-- sgl.query(ListBoats, many)
SELECT id, name, berth, crew, route, fees, contact FROM boats ORDER BY id;

-- sgl.query(BoatSummaries, many)
SELECT
    name,
    crew[1] AS skipper,
    array_length(crew) AS crew_size,
    map_keys(fees) AS months,
    fees['August'] AS august_fee
FROM boats
ORDER BY id;
