-- sgl.migration(no_transaction: true)
CREATE INDEX CONCURRENTLY IF NOT EXISTS menu_price_idx ON menu (price_pence);
