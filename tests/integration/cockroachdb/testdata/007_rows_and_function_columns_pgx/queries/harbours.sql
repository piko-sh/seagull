-- sgl.query(HarbourRows, many)
SELECT h AS harbour, row_to_json(h) AS document FROM harbours h ORDER BY h.id;

-- sgl.query(BerthRowsOuterJoined, many)
SELECT h.id, b AS berth, row_to_json(b) AS berth_document
FROM harbours h LEFT JOIN berths b ON b.harbour_id = h.id
ORDER BY h.id, b.id;

-- sgl.query(FacilityElements, many)
SELECT h.id, f.f AS facility
FROM harbours h, jsonb_array_elements_text(h.facilities) AS f
ORDER BY h.id, f.f;

-- sgl.query(FacilitiesInOrder, many)
SELECT h.id, f, ordinality
FROM harbours h, jsonb_array_elements(h.facilities) WITH ORDINALITY AS f
ORDER BY h.id, ordinality;

-- sgl.query(NumberedSeries, many)
SELECT n.value, n.position FROM generate_series(1, 3) WITH ORDINALITY AS n(value, position) ORDER BY n.position;
