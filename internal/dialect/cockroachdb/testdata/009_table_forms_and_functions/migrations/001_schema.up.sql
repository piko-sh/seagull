CREATE TABLE users (
    id UUID NOT NULL DEFAULT gen_random_ulid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    email STRING NOT NULL,
    email_domain STRING AS (split_part(email, '@', 2)) STORED,
    email_lower STRING AS (lower(email)) VIRTUAL,
    password_hash STRING NOT NULL NOT VISIBLE,
    tags STRING[] NOT NULL DEFAULT ARRAY[],
    settings JSONB NOT NULL DEFAULT '{}',
    PRIMARY KEY (created_at, id) USING HASH,
    UNIQUE INDEX users_email_key (email) STORING (email_domain),
    INVERTED INDEX users_settings_idx (settings),
    INDEX users_domain_idx (email_domain) WHERE email_domain IS NOT NULL,
    FAMILY f1 (id, created_at, email, email_domain, password_hash),
    FAMILY f2 (tags, settings)
);
