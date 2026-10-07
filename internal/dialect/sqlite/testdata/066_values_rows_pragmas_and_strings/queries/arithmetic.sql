-- sgl.query(name: Shares, command: many)
SELECT id, amount / parts AS share, amount % parts AS remainder, rate / parts AS rate_share, amount / 2 AS half,
    amount % 0.5 AS nothing
FROM ledger;
