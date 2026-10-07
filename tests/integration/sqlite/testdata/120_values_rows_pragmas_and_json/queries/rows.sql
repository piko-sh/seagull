-- sgl.query(name: LedgerAfter, command: many)
SELECT id, category FROM ledger WHERE (category, id) > (?, ?) ORDER BY category, id;

-- sgl.query(name: MoveLedger, command: exec)
UPDATE ledger SET (category, dir) = (?, ?) WHERE id = ?;

-- sgl.query(name: LedgerEntry, command: one)
SELECT id, category, dir FROM ledger WHERE id = ?;
