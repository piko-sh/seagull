-- sgl.query(LiteralInSelect, one)
SELECT {'apples': 1} AS counts, {id:UInt64} AS id;

-- sgl.query(LiteralInWhere, many)
SELECT id FROM stock WHERE counts = {'apples': 1};

-- sgl.query(LiteralInInsertSelect, exec)
INSERT INTO stock (id, counts) SELECT {id:UInt64}, {'apples': 1};

-- sgl.query(LiteralInMutation, exec)
ALTER TABLE stock UPDATE counts = {'apples': 1} WHERE id = {id:UInt64};

-- sgl.query(PlaceholderInLiteral, exec)
INSERT INTO stock (id, counts) VALUES ({id:UInt64}, {'kiwis': {kiwis:UInt32}});
