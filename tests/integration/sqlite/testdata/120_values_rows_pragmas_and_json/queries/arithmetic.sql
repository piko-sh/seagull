-- sgl.query(name: Shares, command: many)
SELECT id, amount / parts AS share, amount % parts AS remainder, amount / 2 AS half FROM ledger ORDER BY id;
