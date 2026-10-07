-- sgl.query(SetPrice, exec)
INSERT INTO prices (species, pence_per_kilo) VALUES ($1, $2);

-- sgl.query(LandCatch, one)
SELECT land_catch(:boat, :species, :grams) AS id;

-- sgl.query(SpeciesPrice, one)
SELECT species_price(:species) AS pence_per_kilo;

-- sgl.query(BoatHaul, many)
SELECT haul.species, haul.weight_grams, price_pence(haul.weight_grams, prices.pence_per_kilo) AS value_pence
FROM boat_haul(:boat) AS haul
JOIN prices ON prices.species = haul.species
ORDER BY haul.species;
