-- sgl.query(name: FilterCatches, command: many)
SELECT
    id,
    trim(species, ' ') AS species,
    strftime(caught_at, '%Y-%m-%d') AS caught_on,
    if(weight > 10, 'heavy', 'light') AS size_class,
    uuid() AS request_id
FROM catches
WHERE regexp_matches(species, '^cod')
  AND prefix(species, 'c')
  AND suffix(species, 'd')
  AND caught_at < today();
