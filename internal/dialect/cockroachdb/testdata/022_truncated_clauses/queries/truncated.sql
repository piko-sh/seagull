-- sgl.query(name: OrderByJunk, command: many)
SELECT k FROM a ORDER BY k xx yy;

-- sgl.query(name: EmptyLimit, command: many)
SELECT k FROM a LIMIT;

-- sgl.query(name: EmptyOffset, command: many)
SELECT k FROM a LIMIT 1 OFFSET;

-- sgl.query(name: DanglingAnd, command: many)
SELECT k AND FROM a WHERE k = 1 AND;

-- sgl.query(name: Valid, command: many)
SELECT k FROM a WHERE k IN (1, 2) ORDER BY k LIMIT 1 OFFSET 1;
