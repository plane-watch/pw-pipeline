-- Connect to the target DB
\connect test

-- Optional: ensure the test user owns the public schema (handy for later)
ALTER SCHEMA public OWNER TO test;

-- Create the users table
CREATE TABLE public.users (
    id SERIAL PRIMARY KEY,
    email VARCHAR DEFAULT '' NOT NULL,
    encrypted_password VARCHAR DEFAULT '' NOT NULL,
    reset_password_token VARCHAR,
    reset_password_sent_at TIMESTAMP,
    remember_created_at TIMESTAMP,
    first_name VARCHAR,
    last_name VARCHAR,
    discord_nickname VARCHAR,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    provider VARCHAR,
    uid VARCHAR
);

-- Insert test user
INSERT INTO users (email, encrypted_password, first_name, last_name, created_at, updated_at)
VALUES (
    'test@example.com',
    'supersecret',
    'Test',
    'User',
    NOW(),
    NOW()
);

-- Create the feeder_muxes table
CREATE TABLE public.feeder_muxes (
     id SERIAL PRIMARY KEY,
     name VARCHAR,
     latitude DECIMAL(9,6),
     longitude DECIMAL(9,6),
     output_port INTEGER,
     created_at TIMESTAMP NOT NULL,
     updated_at TIMESTAMP NOT NULL
);

-- Insert test mux
INSERT INTO public.feeder_muxes (name, latitude, longitude, output_port, created_at, updated_at)
VALUES ('Test Mux', -31.950527, 115.860457, 30001, NOW(), NOW());


-- Create the feeders table
CREATE TABLE public.feeders (
    id            SERIAL PRIMARY KEY,
    user_id       INTEGER,
    address       VARCHAR,
    mlat_enabled  BOOLEAN DEFAULT TRUE,
    latitude      DECIMAL(9,6),
    longitude     DECIMAL(9,6),
    feed_direction INTEGER DEFAULT 0,
    feed_protocol  INTEGER DEFAULT 0,
    last_seen     TIMESTAMP,
    api_key       VARCHAR,
    created_at    TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMP NOT NULL DEFAULT NOW(),
    mast_height   DECIMAL DEFAULT 0.0,
    altitude      DECIMAL DEFAULT 0.0,
    feed_host     VARCHAR,
    feed_port     INTEGER,
    feeder_mux_id INTEGER,
    label         VARCHAR,
    feeder_code   VARCHAR
);
ALTER TABLE public.feeders OWNER TO test;

-- Seed / upsert a feeder record:
INSERT INTO public.feeders (
    feeder_code, label, user_id, feeder_mux_id, api_key, mlat_enabled, created_at, updated_at
) VALUES (
    'testfeeder',
    'Test Feeder',
    1,
    1,
    'AA15AFF9-25AC-4FF3-93F8-5C3843353FA7',
    TRUE,
    NOW(),
    NOW()
);

-- Optional: ensure privileges (if not using DB ownership)
GRANT ALL ON SCHEMA public TO test;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO test;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO test;
