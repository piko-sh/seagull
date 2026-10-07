-- sgl.query(RecordProbe, exec)
INSERT INTO probes (id, label, amount, ratio, flag, note, payload, optional_label, optional_amount, tags, address)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- sgl.query(RecordProbes, batch)
INSERT INTO probes (id, label, amount, ratio, flag, note, payload, optional_label, optional_amount, tags, address)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- sgl.query(FindByLabel, many)
SELECT id FROM probes WHERE label = $1 ORDER BY id;

-- sgl.query(FindByAmount, many)
SELECT id FROM probes WHERE amount = $1 ORDER BY id;

-- sgl.query(FindByRatio, many)
SELECT id FROM probes WHERE ratio = $1 ORDER BY id;

-- sgl.query(FindByFlag, many)
SELECT id FROM probes WHERE flag = $1 ORDER BY id;

-- sgl.query(FindByNote, many)
SELECT id FROM probes WHERE note = $1 ORDER BY id;

-- sgl.query(FindByPayload, many)
SELECT id FROM probes WHERE payload = $1 ORDER BY id;

-- sgl.query(FindByOptionalLabel, many)
SELECT id FROM probes WHERE optional_label = $1 ORDER BY id;

-- sgl.query(FindByTags, many)
SELECT id FROM probes WHERE tags = $1 ORDER BY id;

-- sgl.query(FindByAddress, many)
SELECT id FROM probes WHERE address = $1 ORDER BY id;

-- sgl.query(ReadLabel, one)
SELECT label FROM probes WHERE id = $1;

-- sgl.query(ReadAmount, one)
SELECT amount FROM probes WHERE id = $1;

-- sgl.query(ReadRatio, one)
SELECT ratio FROM probes WHERE id = $1;

-- sgl.query(ReadFlag, one)
SELECT flag FROM probes WHERE id = $1;

-- sgl.query(ReadNote, one)
SELECT note FROM probes WHERE id = $1;

-- sgl.query(ReadPayload, one)
SELECT payload FROM probes WHERE id = $1;

-- sgl.query(ReadOptionalLabel, one)
SELECT optional_label FROM probes WHERE id = $1;

-- sgl.query(ReadOptionalAmount, one)
SELECT optional_amount FROM probes WHERE id = $1;

-- sgl.query(ReadTags, one)
SELECT tags FROM probes WHERE id = $1;

-- sgl.query(ReadAddress, one)
SELECT address FROM probes WHERE id = $1;

-- sgl.query(ListProbes, many)
SELECT id, label, amount, ratio, flag, note, payload, optional_label, optional_amount, tags, address FROM probes ORDER BY id;
