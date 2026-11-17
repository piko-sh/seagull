-- sgl.query(name: UseUnknownCallee, command: one)
SELECT calls_unknown() AS result;

-- sgl.query(name: UseKnownPure, command: one)
SELECT calls_known_pure(10) AS result;
