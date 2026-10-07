-- sgl.query(name: CompareAddresses, command: many)
SELECT id FROM hosts WHERE addr = $1::inet OR addr <> CAST($2 AS inet) OR $3::inet = addr OR $4 = addr OR addr = $5::cidr;

-- sgl.query(name: ListAddresses, command: many)
SELECT id FROM hosts WHERE addr IN ($1::inet, CAST($2 AS inet), $3) OR addr NOT IN ($4::cidr);

-- sgl.query(name: BoundAddresses, command: many)
SELECT id FROM hosts WHERE addr BETWEEN $1::inet AND CAST($2 AS inet) OR addr BETWEEN $3 AND $4;

-- sgl.query(name: MatchLabels, command: many)
SELECT id FROM hosts WHERE label = $1::text OR label LIKE $2 OR lower(label) = $3 OR length(label) > $4 OR code = $5::varchar OR code = $6::varchar(4);

-- sgl.query(name: InsertHost, command: exec)
INSERT INTO hosts (id, addr, label, code) VALUES ($1, CAST($2 AS inet), CAST($3 AS varchar), $4::text);

-- sgl.query(name: UpdateHost, command: exec)
UPDATE hosts SET addr = CAST($1 AS inet), label = $2::text, code = CAST($3 AS text) WHERE id = $4;
