-- sgl.query(name: TagPositions, command: many)
SELECT id, array_length(tags, 1) AS tag_count, array_lower(tags, 1) AS first_index,
       array_upper(tags, 1) AS last_index, array_position(tags, 'urgent') AS urgent_at,
       cardinality(tags) AS cardinality
FROM tasks
ORDER BY id;

-- sgl.query(name: TimeSpent, command: many)
SELECT project, sum(spent) AS total, avg(spent) AS average,
       percentile_cont(0.5) WITHIN GROUP (ORDER BY spent) AS median_spent,
       percentile_cont(ARRAY[0.5, 0.9]) WITHIN GROUP (ORDER BY spent) AS spent_percentiles,
       percentile_cont(0.5) WITHIN GROUP (ORDER BY estimate_hours) AS median_estimate,
       percentile_disc(0.5) WITHIN GROUP (ORDER BY spent DESC) AS picked_spent,
       percentile_disc(ARRAY[0.25, 0.75]) WITHIN GROUP (ORDER BY estimate_hours) AS estimate_quartiles,
       mode() WITHIN GROUP (ORDER BY estimate_hours) AS usual_estimate
FROM tasks
GROUP BY project
ORDER BY project;
