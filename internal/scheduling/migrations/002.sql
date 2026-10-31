CREATE TABLE IF NOT EXISTS authorization_bindings (
    model_hash text PRIMARY KEY,
    store_id text NOT NULL,
    model_id text NOT NULL
);
CREATE TABLE IF NOT EXISTS scheduling_bootstrap (
    id integer PRIMARY KEY CHECK(id=1),
    issuer text NOT NULL,
    subject text NOT NULL
);
