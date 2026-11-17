-- sgl.query(name: HarbourDocuments, command: many)
SELECT row_to_json(harbours) AS row_document, to_json(harbours) AS document, to_jsonb(harbours) AS binary_document
FROM harbours;

-- sgl.query(name: HarbourRows, command: many)
SELECT harbours, berths
FROM harbours
LEFT JOIN berths ON berths.harbour_id = harbours.id
WHERE harbours IS NOT NULL
ORDER BY harbours;

-- sgl.query(name: AllHarbours, command: one)
SELECT json_agg(h) AS harbours FROM harbours h;

-- sgl.query(name: NamedHarbours, command: many)
SELECT v FROM harbour_names v;

-- sgl.query(name: CountedRows, command: many)
SELECT counted FROM (SELECT harbour_id, count(*) AS total FROM berths GROUP BY harbour_id) counted;

-- sgl.query(name: PlanItems, command: many)
SELECT item FROM berths, jsonb_array_elements(berths.plan) AS item;

-- sgl.query(name: MooringCounts, command: many)
SELECT moorings FROM moorings;
