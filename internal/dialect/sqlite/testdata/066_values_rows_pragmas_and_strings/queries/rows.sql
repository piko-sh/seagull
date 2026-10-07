-- sgl.query(name: LedgerAfter, command: many)
SELECT id, category FROM ledger WHERE (category, id) > (?, ?) ORDER BY category, id;

-- sgl.query(name: LedgerIn, command: many)
SELECT id FROM ledger WHERE (category, dir) IN ((?, ?), ('b', 'c'));

-- sgl.query(name: MoveLedger, command: exec)
UPDATE ledger SET (category, dir) = (?, ?) WHERE id = ?;
