-- sgl.query(GullPredicates, many)
SELECT id, wing > 40 AS long_wing, span > 100 AS wide, span IS NULL AS unmeasured, span IS NOT NULL AS measured,
    wing IN (40, 42) AS listed, span IN (90, NULL) AS listed_span, wing IN (40, NULL) AS listed_wing,
    span BETWEEN 90 AND 110 AS typical, wing > 40 AND ringed AS long_and_ringed, tagged OR ringed AS marked,
    wing > 40 OR span > 100 AS large, NOT (wing > 40) AS short_wing, NOT tagged AS untagged, EXISTS (SELECT 1) AS present,
    name LIKE 'Her%' AS herring_gull
FROM gulls ORDER BY id;
