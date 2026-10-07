-- sgl.query(ListLegs, many)
SELECT * FROM sightings ARRAY JOIN legs;

-- sgl.query(ListLegFields, many)
SELECT id, legs.colony, sightings.legs.minutes, legs FROM sightings ARRAY JOIN legs;

-- sgl.query(ListAliasedLegs, many)
SELECT leg.colony, leg.minutes, leg, legs.colony FROM sightings ARRAY JOIN legs AS leg;

-- sgl.query(ListOneLegField, many)
SELECT legs.colony, legs.minutes FROM sightings ARRAY JOIN legs.colony;

-- sgl.query(ListTags, many)
SELECT * FROM sightings ARRAY JOIN tags;

-- sgl.query(ListTagsQualified, many)
SELECT s.id, tags FROM sightings AS s ARRAY JOIN s.tags;

-- sgl.query(ListNests, many)
SELECT nest.colony, nest.eggs, nest.1, nest FROM sightings ARRAY JOIN nests AS nest;

-- sgl.query(ListPairs, many)
SELECT pair.1, pair.2 FROM sightings ARRAY JOIN pairs AS pair;

-- sgl.query(ListCountsOrEmpty, many)
SELECT id, tag, amount FROM sightings LEFT ARRAY JOIN tags AS tag, counts AS amount;

-- sgl.query(ListLevels, many)
SELECT level FROM sightings ARRAY JOIN [1, 2, 3] AS level;

-- sgl.query(ListDoubledMinutes, many)
SELECT doubled FROM sightings ARRAY JOIN arrayMap(m -> m * 2, legs.minutes) AS doubled;

-- sgl.query(ListColonyLegs, many)
SELECT id FROM sightings ARRAY JOIN legs WHERE legs.colony = {colony:String} ORDER BY legs.minutes;
