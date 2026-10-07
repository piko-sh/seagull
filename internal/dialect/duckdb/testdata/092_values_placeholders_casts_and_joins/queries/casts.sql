-- sgl.query(name: Tried, command: many)
-- sgl.column(labels, type: "MAP(VARCHAR, INTEGER)")
SELECT id, TRY_CAST(note AS INTEGER) AS number, try(CAST(note AS INTEGER)) AS attempted, CAST(fare AS INTEGER) AS whole,
    CAST(doc AS INTEGER[3]) AS triple, doc AS labels
FROM trips;
