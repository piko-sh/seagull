CREATE FUNCTION price_pence(weight_grams INT, pence_per_kilo INT) RETURNS INT
DETERMINISTIC NO SQL
RETURN weight_grams * pence_per_kilo / 1000;

CREATE FUNCTION land_catch(boat_name VARCHAR(50), caught VARCHAR(50), grams INT) RETURNS BIGINT
DETERMINISTIC MODIFIES SQL DATA
BEGIN
    INSERT INTO catches (boat, species, weight_grams) VALUES (boat_name, caught, grams);
    RETURN LAST_INSERT_ID();
END;

-- sgl.migration(readonly: true)
CREATE FUNCTION species_price(wanted VARCHAR(50)) RETURNS INT
DETERMINISTIC MODIFIES SQL DATA
RETURN (SELECT pence_per_kilo FROM prices WHERE species = wanted);
