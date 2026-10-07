-- sgl.query(name: ListPlain, command: many)
SELECT * FROM plain ORDER BY note;

-- sgl.query(name: ListPairs, command: many)
SELECT * FROM pairs ORDER BY note;

-- sgl.query(name: ListStrict, command: many)
SELECT * FROM strict_keys ORDER BY code;

-- sgl.query(name: ListNumbered, command: many)
SELECT * FROM numbered ORDER BY id;

-- sgl.query(name: ListPlainView, command: many)
SELECT * FROM plain_view ORDER BY plain_note;
