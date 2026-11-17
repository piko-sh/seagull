-- sgl.query(name: SortedOdd, command: many)
-- sgl.sortable(order_by, columns: ["Mixed", "select"])
SELECT id, "Mixed", "select" FROM items

-- sgl.query(name: Sampled, command: many, dynamic: runtime)
SELECT id, code FROM items TABLESAMPLE SYSTEM (100)
