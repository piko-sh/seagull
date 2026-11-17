-- sgl.query(name: BareAlias, command: many)
SELECT t.id FROM "from" t;

-- sgl.query(name: BareAliasBackticked, command: many)
SELECT t.id FROM `FROM` t;

-- sgl.query(name: BareAliasJoined, command: many)
SELECT t.id FROM other o JOIN "from" t ON o.id = t.id;
