-- sgl.query(OrderedTags, many)
SELECT id, tags FROM items ORDER BY items.tags, items.id;

-- sgl.query(DistinctTags, many)
SELECT d.tags FROM (SELECT DISTINCT tags FROM items) AS d ORDER BY d.tags;

-- sgl.query(UnionTags, many)
SELECT u.tags FROM (SELECT tags FROM items UNION SELECT other FROM items) AS u ORDER BY u.tags;

-- sgl.query(GroupedTags, many)
SELECT tags, count(*) AS n FROM items GROUP BY tags ORDER BY items.tags;
