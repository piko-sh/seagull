-- sgl.query(AddLedger, exec)
INSERT INTO ledgers (id, ident, price, refund, opens, closes, flags, mask, span, moments, days, amounts, blobs, rates)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14);

-- sgl.query(GetLedger, one)
SELECT id, ident, price, refund, opens, closes, flags, mask, span, moments, days, amounts, blobs, rates
FROM ledgers
WHERE id = $1;

-- sgl.query(SetRates, exec)
UPDATE ledgers SET rates = $1 WHERE id = $2;

-- sgl.query(LedgersPricedAt, many)
SELECT id, price FROM ledgers WHERE price = $1 ORDER BY id;

-- sgl.query(LedgerByIdent, one)
SELECT id FROM ledgers WHERE ident = $1;
