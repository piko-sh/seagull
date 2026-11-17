CREATE TABLE items (id BIGINT NOT NULL, label VARCHAR NOT NULL, note VARCHAR);
CREATE VIEW renamed_items (item_id) AS SELECT id, label, note FROM items;
