-- sgl.query(name: ListAccounts, command: many)
SELECT id, email, score, tier FROM accounts;

-- sgl.query(name: ListAccountScores, command: many)
SELECT id, email, doubled FROM account_scores;
