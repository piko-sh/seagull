-- sgl.query(AddPrice, exec)
INSERT INTO prices (id, amount, discount, label) VALUES (?, ?, ?, ?);

-- sgl.query(AddPrices, batch)
INSERT INTO prices (id, amount, discount, label) VALUES (?, ?, ?, ?);

-- sgl.query(CopyPrices, copyfrom)
INSERT INTO prices (id, amount, discount, label) VALUES (?, ?, ?, ?);

-- sgl.query(SetDiscount, execrows)
UPDATE prices SET discount = ? WHERE id = ?;

-- sgl.query(PricesWithDiscount, many)
SELECT id FROM prices WHERE discount = ? ORDER BY id;

-- sgl.query(PricesIn, many)
-- ?1 as sgl.param(amounts, kind: slice)
SELECT id FROM prices WHERE amount IN (?1) ORDER BY id;

-- sgl.query(SearchPrices, many)
-- ?1 as sgl.param(max_amount, optional: true)
-- ?2 as sgl.param(max_discount, optional: true)
SELECT id FROM prices WHERE (?1 IS NULL OR amount <= ?1) AND (?2 IS NULL OR discount <= ?2) ORDER BY id;

-- sgl.query(name: BrowsePrices, command: many, dynamic: runtime)
SELECT id, amount, discount FROM prices WHERE discount >= ?;

-- sgl.query(ListPrices, many)
SELECT id, amount, discount, coalesce(discount, ?) AS effective FROM prices ORDER BY id;
