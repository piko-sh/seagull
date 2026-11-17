-- sgl.query(ColumnsInWhere, many)
SELECT id FROM items WHERE id NOT IN (a, b);

-- sgl.query(QualifiedColumnInWhere, many)
SELECT i.id FROM items i WHERE i.id IN (i.a, 3);

-- sgl.query(ColumnsInSelectList, many)
SELECT id IN (a, b) AS member FROM items;

-- sgl.query(ColumnInNestedQuery, many)
SELECT id FROM items WHERE id IN (SELECT a FROM items WHERE b IN (a, 1));

-- sgl.query(ConstantList, many)
SELECT id FROM items WHERE id IN (1, 2 + 3, {p:Int64});

-- sgl.query(SubqueryList, many)
SELECT id FROM items WHERE id IN (SELECT a FROM items);
