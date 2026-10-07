-- sgl.query(ListSightings, many)
SELECT sightings.species, sightings.count, members.name AS member, members.email
FROM sightings
JOIN members ON members.id = sightings.member_id
ORDER BY sightings.seen_at;

-- sgl.query(BirdsBySpecies, many)
SELECT species, sum(count) AS birds FROM sightings GROUP BY species ORDER BY species;
