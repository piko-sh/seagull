CREATE TABLE databases (account_id TEXT NOT NULL, region TEXT NOT NULL, arn TEXT NOT NULL, name TEXT, UNIQUE (account_id, region, arn) ON CONFLICT REPLACE);
CREATE TABLE engines (engine TEXT, version TEXT, PRIMARY KEY (engine) ON CONFLICT IGNORE, CONSTRAINT engine_version UNIQUE (engine, version) ON CONFLICT ABORT);
