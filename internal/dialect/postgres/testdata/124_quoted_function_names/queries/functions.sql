-- sgl.query(name: QuotedLabel, command: one)
SELECT "Label"(2) AS doubled;

-- sgl.query(name: UnquotedLabel, command: one)
SELECT label(2) AS labelled;

-- sgl.query(name: UpperCaseLabel, command: one)
SELECT LABEL(2) AS shouted;

-- sgl.query(name: QuotedScale, command: one)
SELECT "Scale"($1) AS scaled;

-- sgl.query(name: UnquotedScale, command: one)
SELECT scale($1) AS scaled;

-- sgl.query(name: QuotedSeries, command: many)
SELECT * FROM "Series"();

-- sgl.query(name: UnquotedSeries, command: many)
SELECT * FROM series();

-- sgl.query(name: QuotedPurge, command: one)
SELECT "Purge"() AS purged;

-- sgl.query(name: UnquotedPurge, command: one)
SELECT purge() AS remaining;

-- sgl.query(name: Counted, command: one)
SELECT counted() AS remaining;
