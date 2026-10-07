-- sgl.query(RegisterBoat, execrows)
INSERT IGNORE INTO boats (registration, name, kit) VALUES (?, ?, ?);

-- sgl.query(LogCrossing, execrows)
UPDATE boats SET crossings = crossings + 1 WHERE registration = ?;

-- sgl.query(SignOn, exec)
INSERT INTO crew (registration, name, `rank`) VALUES (?, ?, ?);

-- sgl.query(AssignBerth, execrows)
REPLACE INTO berths (berth, registration) VALUES (?, ?);

-- sgl.query(ListBerths, many)
SELECT berths.berth, boats.name
FROM berths
JOIN boats ON boats.registration = berths.registration
ORDER BY berths.berth;

-- sgl.query(BoatsWithKit, many)
-- ?1 as sgl.param(item, type: text)
SELECT registration, name, kit FROM boats WHERE FIND_IN_SET(?1, kit) > 0 ORDER BY registration;

-- sgl.query(Harbour, many)
SELECT
    boats.registration,
    boats.name,
    boats.kit,
    boats.crossings,
    GROUP_CONCAT(CONCAT(crew.name, ' the ', crew.`rank`) ORDER BY crew.id SEPARATOR ', ') AS crew
FROM boats
LEFT JOIN crew ON crew.registration = boats.registration
GROUP BY boats.registration
ORDER BY boats.registration;
