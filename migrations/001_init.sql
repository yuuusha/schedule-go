CREATE TABLE IF NOT EXISTS resources (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL CHECK(length(trim(name))>0),
    type TEXT NOT NULL CHECK(length(trim(type))>0),
    description TEXT NOT NULL DEFAULT '',
    location TEXT NOT NULL DEFAULT '',
    capacity INT NOT NULL DEFAULT 1 CHECK(capacity>0),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL CHECK(length(trim(name))>0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS bookings (
    id BIGSERIAL PRIMARY KEY,
    resource_id BIGINT NOT NULL REFERENCES resources(id) ON DELETE RESTRICT,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    start_at TIMESTAMPTZ NOT NULL,
    end_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','canceled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    canceled_at TIMESTAMPTZ,
    CHECK(start_at < end_at),
    CHECK((status='active' AND canceled_at IS NULL) OR (status='canceled' AND canceled_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS bookings_resource_interval_idx ON bookings(resource_id,start_at,end_at) WHERE status='active';
CREATE INDEX IF NOT EXISTS bookings_user_status_start_idx ON bookings(user_id,status,start_at);
CREATE INDEX IF NOT EXISTS bookings_resource_status_idx ON bookings(resource_id,status);
