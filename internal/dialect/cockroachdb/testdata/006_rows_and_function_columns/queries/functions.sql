-- sgl.query(FacilityElements, many)
SELECT f FROM harbours, jsonb_array_elements(harbours.facilities) AS f;

-- sgl.query(FacilityElementsQualified, many)
SELECT harbours.id, f.f FROM harbours, jsonb_array_elements(harbours.facilities) AS f;

-- sgl.query(FacilityElementsStar, many)
SELECT * FROM harbours, jsonb_array_elements_text(harbours.facilities) AS f;

-- sgl.query(FacilityElementsUnaliased, many)
SELECT value FROM jsonb_array_elements('[1, 2]'::JSONB);

-- sgl.query(FacilityElementsNumbered, many)
SELECT f, ordinality FROM harbours, jsonb_array_elements(harbours.facilities) WITH ORDINALITY AS f;

-- sgl.query(BerthNumbers, many)
SELECT n FROM harbours, unnest(harbours.berth_numbers) AS n;

-- sgl.query(BerthNumbersNumbered, many)
SELECT n.number, n.position FROM harbours, unnest(harbours.berth_numbers) WITH ORDINALITY AS n(number, position);

-- sgl.query(FacilityPairs, many)
SELECT pair.key, pair.value FROM harbours, jsonb_each(harbours.facilities) AS pair;
