-- sgl.query(name: AliasAfterAs, command: many)
SELECT t.id FROM "from" AS t;

-- sgl.query(name: NoAlias, command: many)
SELECT id FROM "from" WHERE id = 1;

-- sgl.query(name: OtherKeywordBareAlias, command: many)
SELECT t.id FROM "select" t;
