-- sgl.query(AddPrice, exec)
INSERT INTO prices (id, amount, discount, label) VALUES ({id:UInt64}, {amount:Decimal(10, 2)}, {discount:Nullable(Decimal(10, 2))}, {label:Nullable(String)});

-- sgl.query(AddPrices, batch)
INSERT INTO prices (id, amount, discount, label) VALUES ({id:UInt64}, {amount:Decimal(10, 2)}, {discount:Nullable(Decimal(10, 2))}, {label:Nullable(String)});

-- sgl.query(CopyPrices, copyfrom)
INSERT INTO prices (id, amount, discount, label) VALUES ({id:UInt64}, {amount:Decimal(10, 2)}, {discount:Nullable(Decimal(10, 2))}, {label:Nullable(String)});

-- sgl.query(PricesWithDiscount, many)
SELECT id FROM prices WHERE discount = {discount:Nullable(Decimal(10, 2))} ORDER BY id;

-- sgl.query(PricesIn, many)
-- {amounts:Decimal(10, 2)} as sgl.param(amounts, kind: slice)
SELECT id FROM prices WHERE amount IN ({amounts:Decimal(10, 2)}) ORDER BY id;

-- sgl.query(SearchPrices, many)
-- {max_amount:Nullable(Decimal(10, 2))} as sgl.param(max_amount, optional: true)
-- {max_discount:Nullable(Decimal(10, 2))} as sgl.param(max_discount, optional: true)
SELECT id FROM prices
WHERE ({max_amount:Nullable(Decimal(10, 2))} IS NULL OR amount <= {max_amount:Nullable(Decimal(10, 2))})
  AND ({max_discount:Nullable(Decimal(10, 2))} IS NULL OR discount <= {max_discount:Nullable(Decimal(10, 2))})
ORDER BY id;

-- sgl.query(name: BrowsePrices, command: many, dynamic: runtime)
SELECT id, amount, discount FROM prices WHERE discount >= {discount:Nullable(Decimal(10, 2))}

-- sgl.query(ListPrices, many)
SELECT id, amount, discount, coalesce(discount, {fallback:Nullable(Decimal(10, 2))}) AS effective FROM prices ORDER BY id;
