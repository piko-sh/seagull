-- sgl.query(SetPrice, exec)
INSERT INTO prices (species, pence_per_kilo) VALUES (?, ?);

-- sgl.query(LandCatch, one)
-- ?1 as sgl.param(boat)
-- ?2 as sgl.param(species)
-- ?3 as sgl.param(grams)
SELECT land_catch(?1, ?2, ?3) AS id;

-- sgl.query(SpeciesPrice, one)
-- ?1 as sgl.param(species)
SELECT species_price(?1) AS pence_per_kilo;

-- sgl.query(BoatHaul, many)
SELECT catches.species, catches.weight_grams, price_pence(catches.weight_grams, prices.pence_per_kilo) AS value_pence
FROM catches
JOIN prices ON prices.species = catches.species
WHERE catches.boat = ?
ORDER BY catches.species;
