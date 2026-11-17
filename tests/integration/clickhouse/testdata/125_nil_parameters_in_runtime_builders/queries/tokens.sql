-- sgl.query(AddToken, exec)
INSERT INTO tokens (id, owner, amount, note) VALUES ({id:UInt64}, {owner:Nullable(UUID)}, {amount:Nullable(Decimal(10, 2))}, {note:Nullable(String)});

-- sgl.query(AddTokens, batch)
INSERT INTO tokens (id, owner, amount, note) VALUES ({id:UInt64}, {owner:Nullable(UUID)}, {amount:Nullable(Decimal(10, 2))}, {note:Nullable(String)});

-- sgl.query(name: TokensOf, command: many, dynamic: runtime)
SELECT id FROM tokens WHERE owner = {owner:Nullable(UUID)}

-- sgl.query(name: TokensWorth, command: many, dynamic: runtime)
SELECT id FROM tokens WHERE amount >= {amount:Nullable(Decimal(10, 2))}

-- sgl.query(name: TokensNoted, command: many, dynamic: runtime)
SELECT id FROM tokens WHERE note = {note:Nullable(String)}

-- sgl.query(ListTokens, many)
SELECT id, owner, amount, note FROM tokens ORDER BY id;
