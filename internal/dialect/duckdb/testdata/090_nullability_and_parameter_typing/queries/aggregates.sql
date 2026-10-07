-- sgl.query(name: BookCount, command: one)
SELECT count(*) AS total, (SELECT count(*) FROM authors) AS authors, list(id) AS ids, list(price) AS prices FROM books;

-- sgl.query(name: LabelKeys, command: many)
SELECT id, map_keys(labels) AS label_keys, map_values(labels) AS label_values FROM books;
