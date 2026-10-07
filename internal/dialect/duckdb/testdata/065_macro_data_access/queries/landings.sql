-- sgl.query(name: MacroUntyped, command: many)
SELECT harbour, tonnes(kg) AS tonnes FROM landings;

-- sgl.query(name: ShoutedSpecies, command: many)
SELECT shout(species) AS species FROM landings;

-- sgl.query(name: NextLandingID, command: one)
SELECT next_landing_id() AS id;

-- sgl.query(name: TableMacro, command: many)
SELECT species, sum(kg) AS kg FROM harbour_landings($1) GROUP BY species;

-- sgl.query(name: TableMacroColumns, command: many)
SELECT * FROM harbour_landings($1);
