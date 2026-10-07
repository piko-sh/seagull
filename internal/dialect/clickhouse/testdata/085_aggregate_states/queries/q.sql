-- sgl.query(Merged, many)
SELECT species, uniqMerge(a) AS sighted, sumMerge(b) AS kilos, maxMerge(c) AS heaviest, countMerge(d) AS trips, groupArray(species) AS names FROM totals GROUP BY species;
