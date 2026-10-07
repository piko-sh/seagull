-- sgl.query(name: BoatsTagged, command: many)
SELECT id, FIND_IN_SET(?, tags) AS position FROM boats WHERE FIND_IN_SET('fast', tags) > 0;
