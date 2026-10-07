-- sgl.query(name: RenamedStarCTE, command: many)
WITH t (`key`, label) AS (SELECT * FROM authors) SELECT `key`, label FROM t;
