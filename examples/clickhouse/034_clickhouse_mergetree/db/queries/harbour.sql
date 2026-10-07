-- sgl.query(LandCatch, exec)
INSERT INTO landings (id, day, boat, species, kilos)
VALUES ({id:UInt64}, {day:Date}, {boat:String}, {species:String}, {kilos:UInt32});

-- sgl.query(MoveBoat, exec)
INSERT INTO berths (boat, berth, version) VALUES ({boat:String}, {berth:String}, {version:UInt32});

-- sgl.query(CurrentBerths, many)
SELECT boat, berth, version FROM berths FINAL ORDER BY boat;

-- sgl.query(TallyCatch, exec)
INSERT INTO daily_catch (day, species, kilos) VALUES ({day:Date}, {species:String}, {kilos:UInt64});

-- sgl.query(DailyCatch, many)
SELECT day, species, kilos FROM daily_catch FINAL ORDER BY day, species;

-- sgl.query(RollUpDay, exec)
INSERT INTO species_stats
SELECT species, sumState(toUInt64(kilos)), maxState(kilos)
FROM landings
WHERE day = {day:Date}
GROUP BY species;

-- sgl.query(SpeciesStats, many)
SELECT species, sumMerge(kilos) AS kilos, maxMerge(heaviest) AS heaviest
FROM species_stats
GROUP BY species
ORDER BY species;

-- sgl.query(BigLandings, many)
SELECT id, boat, kilos
FROM landings
PREWHERE species = {species:String}
WHERE kilos >= {min_kilos:UInt32}
ORDER BY id;
