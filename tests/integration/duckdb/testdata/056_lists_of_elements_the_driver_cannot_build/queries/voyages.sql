-- sgl.query(AddVoyage, exec)
INSERT INTO voyages (id, moods, fares, photos, crew, legs, gusts, watches, pair, forecast)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- sgl.query(AddVoyages, batch)
INSERT INTO voyages (id, moods, fares, photos, crew, legs, gusts, watches, pair, forecast)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- sgl.query(SetForecast, exec)
UPDATE voyages SET forecast = $1 WHERE id = $2;

-- sgl.query(VoyagesSharingAMood, many)
SELECT id FROM voyages WHERE list_has_any(moods, $1::mood[]) ORDER BY id;

-- sgl.query(SetPrice, exec)
-- $1 as sgl.param(amount)
-- $2 as sgl.param(currency)
UPDATE voyages SET price = {'amount': $1::DECIMAL(10, 2), 'currency': $2::VARCHAR} WHERE id = $3;

-- sgl.query(SetRates, exec)
-- $1 as sgl.param(fare_names)
-- $2 as sgl.param(fares)
UPDATE voyages SET rates = map($1::VARCHAR[], $2::DECIMAL(10, 2)[]) WHERE id = $3;

-- sgl.query(GetVoyage, one)
SELECT id, moods, fares, photos, crew, legs, gusts, watches, pair, forecast, price, rates FROM voyages WHERE id = $1;
