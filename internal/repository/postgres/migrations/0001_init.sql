-- gen_random_uuid() is built into PostgreSQL 13+.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE teams (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Names are not unique: a team is joined with its invite code, never by name.
    name        VARCHAR(100) NOT NULL,
    -- Secret shared by members to let others join (122 random bits).
    invite_code CHAR(32)     NOT NULL DEFAULT replace(gen_random_uuid()::text, '-', ''),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_teams_invite_code UNIQUE (invite_code)
);

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id       UUID         NOT NULL REFERENCES teams (id) ON DELETE RESTRICT,
    name          VARCHAR(100) NOT NULL,
    email         VARCHAR(255) NOT NULL,
    password_hash TEXT         NOT NULL,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uq_users_email_lower ON users (lower(email));
CREATE INDEX idx_users_team_id ON users (team_id);

CREATE TABLE tasks (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id    UUID         NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    assignee_id UUID         REFERENCES users (id) ON DELETE SET NULL,
    title       VARCHAR(255) NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    status      VARCHAR(20)  NOT NULL DEFAULT 'todo'
        CONSTRAINT chk_tasks_status CHECK (status IN ('todo', 'in_progress', 'done')),
    due_date    TIMESTAMPTZ,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
-- Listing: "my tasks" ordered by newest, optionally filtered by status.
CREATE INDEX idx_tasks_owner_created ON tasks (owner_id, created_at DESC, id DESC);
CREATE INDEX idx_tasks_owner_status_created ON tasks (owner_id, status, created_at DESC);
-- Listing: tasks assigned to me.
CREATE INDEX idx_tasks_assignee_created ON tasks (assignee_id, created_at DESC) WHERE assignee_id IS NOT NULL;
-- Search by title with ILIKE '%term%'.
CREATE INDEX idx_tasks_title_trgm ON tasks USING gin (title gin_trgm_ops);

CREATE TABLE task_logs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id    UUID        NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    actor_id   UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    action     VARCHAR(50) NOT NULL,
    old_value  JSONB,
    new_value  JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_task_logs_task_created ON task_logs (task_id, created_at DESC);

CREATE TABLE idempotency_keys (
    user_id         UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    idempotency_key UUID        NOT NULL,
    request_hash    CHAR(64)    NOT NULL,
    -- Snapshot of the first successful result, NULL while the request is in flight.
    result          JSONB,
    created_at      TIMESTAMPTZ NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, idempotency_key)
);
CREATE INDEX idx_idempotency_keys_expires_at ON idempotency_keys (expires_at);
