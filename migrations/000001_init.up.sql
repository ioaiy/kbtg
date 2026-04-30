CREATE TABLE IF NOT EXISTS users (
    id      BIGINT PRIMARY KEY,
    balance NUMERIC(20, 2) NOT NULL DEFAULT 0
        CHECK (balance >= 0)
);

CREATE TABLE IF NOT EXISTS balance_history (
    id             BIGSERIAL PRIMARY KEY,
    user_id        BIGINT         NOT NULL REFERENCES users(id),
    amount         NUMERIC(20, 2) NOT NULL CHECK (amount > 0),
    balance_before NUMERIC(20, 2) NOT NULL,
    balance_after  NUMERIC(20, 2) NOT NULL,
    created_at     TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_balance_history_user_created
    ON balance_history (user_id, created_at DESC);

INSERT INTO users (id, balance)
VALUES (1, 100.00)
ON CONFLICT (id) DO NOTHING;
