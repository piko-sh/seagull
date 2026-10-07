CREATE FUNCTION price_pence(weight_grams INTEGER, pence_per_kilo INTEGER) RETURNS INTEGER
LANGUAGE sql IMMUTABLE STRICT
AS $$ SELECT weight_grams * pence_per_kilo / 1000 $$;

CREATE FUNCTION boat_haul(boat_name TEXT)
RETURNS TABLE (species TEXT, weight_grams INTEGER)
LANGUAGE sql STABLE
AS $$ SELECT species, weight_grams FROM catches WHERE boat = boat_name $$;

CREATE FUNCTION land_catch(boat_name TEXT, caught TEXT, grams INTEGER) RETURNS BIGINT
LANGUAGE plpgsql VOLATILE
AS $$
DECLARE
    landed BIGINT;
BEGIN
    INSERT INTO catches (boat, species, weight_grams) VALUES (boat_name, caught, grams)
    RETURNING id INTO landed;
    RETURN landed;
END;
$$;

-- sgl.migration(readonly: true)
CREATE FUNCTION species_price(wanted TEXT) RETURNS INTEGER
LANGUAGE sql VOLATILE
AS $$ SELECT pence_per_kilo FROM prices WHERE species = wanted $$;
