-- sgl.query(name: Columns, command: many)
SELECT quoted_mood, plain_mood, shouted_mood, quoted_moods, quoted_pair, plain_pair FROM readings;

-- sgl.query(name: ColonCasts, command: one)
SELECT 'Happy'::"Mood" AS quoted, 'calm'::mood AS plain, 'bored'::MOOD AS shouted;

-- sgl.query(name: CastFunctions, command: one)
SELECT CAST('Sad' AS "Mood") AS quoted, CAST('tense' AS mood) AS plain;

-- sgl.query(name: QualifiedCasts, command: one)
SELECT ROW(1, 2)::"Shapes"."Pair" AS quoted, ROW('one', 1)::"Shapes".pair AS plain;

-- sgl.query(name: CastParameters, command: many)
SELECT id FROM readings WHERE quoted_mood = $1::"Mood" AND plain_mood = CAST($2 AS mood);

-- sgl.query(name: QuotedCastParameter, command: many)
SELECT id FROM readings WHERE quoted_mood = CAST($1 AS "Mood");

-- sgl.query(name: FunctionTypes, command: many)
SELECT "Swap"(quoted_pair) AS swapped, settle(quoted_mood) AS settled, unsettle(plain_mood) AS unsettled FROM readings;

-- sgl.query(name: FunctionParameters, command: one)
SELECT settle($1) AS settled, unsettle($2) AS unsettled;

-- sgl.query(name: ArrayCasts, command: one)
SELECT ARRAY['Happy']::"Mood"[] AS quoted, ARRAY['calm']::mood[] AS plain;

-- sgl.query(name: ColumnDefinitions, command: many)
SELECT x.quoted, x.plain FROM json_to_record($1) AS x (quoted "Mood", plain mood);

-- sgl.query(name: QuotedBuiltinSpellings, command: one)
SELECT corner, spot, ROW(1, 2)::"Point" AS quoted, '(1,2)'::point AS plain, '(3,4)'::"point" AS quoted_lower
FROM readings
WHERE id = $1;
