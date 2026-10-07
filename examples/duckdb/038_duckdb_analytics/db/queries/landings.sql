-- sgl.query(LandFish, exec)
INSERT INTO landings (harbour, boat, species, kg, landed_on) VALUES ($1, $2, $3, $4, $5);

-- sgl.query(BiggestLandingPerHarbour, many)
SELECT harbour, boat, species, kg, landed_on
FROM landings
QUALIFY row_number() OVER (PARTITION BY harbour ORDER BY kg DESC) = 1
ORDER BY harbour;

-- sgl.query(CatchBySpecies, many)
SELECT
    harbour,
    coalesce(sum(kg) FILTER (WHERE species = 'cod'), 0) AS cod_kg,
    coalesce(sum(kg) FILTER (WHERE species = 'hake'), 0) AS hake_kg,
    coalesce(sum(kg) FILTER (WHERE species = 'mackerel'), 0) AS mackerel_kg
FROM landings
GROUP BY harbour
ORDER BY harbour;

-- sgl.query(CatchPivot, many)
SELECT *
FROM (PIVOT landings ON species IN ('cod', 'hake', 'mackerel') USING sum(kg) GROUP BY harbour)
ORDER BY harbour;

-- sgl.query(TonnesPerBoat, many)
SELECT boat, tonnes(sum(kg)) AS tonnes
FROM landings
GROUP BY boat
ORDER BY boat;

-- sgl.query(HarbourCatch, many)
-- $1 as sgl.param(harbour, type: varchar)
SELECT species, CAST(sum(kg) AS BIGINT) AS kg
FROM harbour_landings($1)
GROUP BY species
ORDER BY species;

-- sgl.query(LandingValues, many)
-- $1 as sgl.param(price_file, type: varchar)
SELECT landings.boat, CAST(sum(landings.kg * prices.pence_per_kg) AS BIGINT) AS value_pence
FROM landings
JOIN read_parquet($1) AS prices ON prices.species = landings.species
GROUP BY landings.boat
ORDER BY landings.boat;

-- sgl.query(AuctionPrices, many)
-- $1 as sgl.param(price_file, type: varchar)
SELECT CAST(species AS VARCHAR) AS species, CAST(pence_per_kg AS INTEGER) AS pence_per_kg
FROM read_parquet($1)
ORDER BY species;
