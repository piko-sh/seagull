-- sgl.query(Insert, exec)
INSERT INTO prices (id, amount) VALUES ({id:UInt64}, {amount:Decimal(18, 4)});

-- sgl.query(Get, one)
SELECT id, amount FROM prices WHERE id = {id:UInt64};
