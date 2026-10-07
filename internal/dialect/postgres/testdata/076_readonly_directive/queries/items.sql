-- sgl.query(name: BareReadOnly, command: many, readonly: true)
SELECT id, name FROM items;

-- sgl.query(name: ExplicitReadOnlyTrue, command: many, readonly: true)
SELECT id, name FROM items;

-- sgl.query(name: ExplicitReadOnlyFalse, command: many, readonly: false)
SELECT id, name FROM items;

-- sgl.query(name: OverrideVolatileToReadOnly, command: one, readonly: true)
SELECT volatile_func($1::integer) AS result;

-- sgl.query(name: InsertOverriddenToReadOnly, command: exec, readonly: true)
INSERT INTO items (name, quantity) VALUES ($1, $2);

-- sgl.query(name: MigrationOverriddenReadOnly, command: one)
SELECT overridden_readonly_func($1::integer) AS result;

-- sgl.query(name: MigrationOverriddenNotReadOnly, command: one)
SELECT overridden_not_readonly_func($1::integer) AS result;
