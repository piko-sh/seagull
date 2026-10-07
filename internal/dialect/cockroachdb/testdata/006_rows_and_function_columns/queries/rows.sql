-- sgl.query(HarbourRows, many)
SELECT h AS harbour, row_to_json(h) AS document, to_jsonb(h) AS binary_document FROM harbours h;

-- sgl.query(BerthRowsOuterJoined, many)
SELECT h.id, b AS berth, row_to_json(b) AS berth_document
FROM harbours h LEFT JOIN berths b ON b.harbour_id = h.id;

-- sgl.query(HarbourDocuments, one)
SELECT json_agg(h) AS harbours FROM harbours h;

-- sgl.query(DerivedRows, many)
SELECT d FROM (SELECT id, name FROM harbours) AS d;
