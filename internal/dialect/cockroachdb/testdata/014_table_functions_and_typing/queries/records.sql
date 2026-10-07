-- sgl.query(name: Populate, command: one)
SELECT * FROM jsonb_populate_record(NULL::results, $1);

-- sgl.query(name: PopulatePairs, command: many)
SELECT p.a, p.b FROM json_populate_recordset(NULL::pair, $1) AS p;

-- sgl.query(name: Numbered, command: many)
SELECT * FROM unnest($1::integer[], $2::text[]) WITH ORDINALITY AS u(n, label, position);
