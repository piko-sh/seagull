-- sgl.query(name: GetOrders, command: many)
SELECT id, "group", "check" FROM "order" WHERE "where" = $1 AND "select" = $2;

-- sgl.query(name: MoveOrder, command: execrows)
UPDATE "order" SET "group" = $1 WHERE "where" = $2;

-- sgl.query(name: AllOrders, command: many)
SELECT * FROM "order";
