CREATE TABLE apps (
  id bigint PRIMARY KEY,
  name text NOT NULL
);

CREATE TABLE app_items (
  id bigserial PRIMARY KEY,
  app_id bigint NOT NULL,
  master_item_id bigint NOT NULL
);
