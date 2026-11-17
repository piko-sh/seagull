-- sgl.query(RecordProbe, exec)
INSERT INTO probes (id, label, amount, ratio, flag, note, payload, optional_label, optional_amount)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- sgl.query(RecordProbes, batch)
INSERT INTO probes (id, label, amount, ratio, flag, note, payload, optional_label, optional_amount)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- sgl.query(FindByLabel, many)
SELECT id FROM probes WHERE label = ? ORDER BY id;

-- sgl.query(FindByAmount, many)
SELECT id FROM probes WHERE amount = ? ORDER BY id;

-- sgl.query(FindByRatio, many)
SELECT id FROM probes WHERE ratio = ? ORDER BY id;

-- sgl.query(FindByFlag, many)
SELECT id FROM probes WHERE flag = ? ORDER BY id;

-- sgl.query(FindByNote, many)
SELECT id FROM probes WHERE note = ? ORDER BY id;

-- sgl.query(FindByPayload, many)
SELECT id FROM probes WHERE payload = ? ORDER BY id;

-- sgl.query(FindByOptionalLabel, many)
SELECT id FROM probes WHERE optional_label = ? ORDER BY id;

-- sgl.query(ReadLabel, one)
SELECT label FROM probes WHERE id = ?;

-- sgl.query(ReadAmount, one)
SELECT amount FROM probes WHERE id = ?;

-- sgl.query(ReadRatio, one)
SELECT ratio FROM probes WHERE id = ?;

-- sgl.query(ReadFlag, one)
SELECT flag FROM probes WHERE id = ?;

-- sgl.query(ReadNote, one)
SELECT note FROM probes WHERE id = ?;

-- sgl.query(ReadPayload, one)
SELECT payload FROM probes WHERE id = ?;

-- sgl.query(ReadOptionalLabel, one)
SELECT optional_label FROM probes WHERE id = ?;

-- sgl.query(ReadOptionalAmount, one)
SELECT optional_amount FROM probes WHERE id = ?;

-- sgl.query(ListProbes, many)
SELECT id, label, amount, ratio, flag, note, payload, optional_label, optional_amount FROM probes ORDER BY id;
