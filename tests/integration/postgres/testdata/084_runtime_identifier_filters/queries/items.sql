-- sgl.query(name: CastAlias, command: many, dynamic: runtime)
SELECT id, CAST(code AS text) AS code_text FROM items

-- sgl.query(name: OddNames, command: many, dynamic: runtime)
SELECT id, "Mixed", "select", "a.b" FROM items
