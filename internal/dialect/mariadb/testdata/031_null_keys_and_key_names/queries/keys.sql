-- sgl.query(name: Keys, command: many)
SELECT null_key.id AS null_key_id, modified_key.id AS modified_key_id, gauges.minimum FROM null_key, modified_key, gauges;
