CREATE TYPE transaction_status AS ENUM ('pending', 'settled', 'failed');

CREATE TABLE
    accounts (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        owner TEXT NOT NULL,
        currency TEXT NOT NULL,
        created_at TIMESTAMPTZ NOT NULL DEFAULT now ()
    );

CREATE TABLE
    transactions (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        idempotency_key TEXT NOT NULL UNIQUE,
        type TEXT NOT NULL,
        status transaction_status NOT NULL DEFAULT 'pending',
        created_at TIMESTAMPTZ NOT NULL DEFAULT now ()
    );

CREATE TABLE
    entries (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        transaction_id UUID NOT NULL REFERENCES transactions (id),
        account_id UUID NOT NULL REFERENCES accounts (id),
        amount BIGINT NOT NULL,
        created_at TIMESTAMPTZ NOT NULL DEFAULT now ()
    );

CREATE INDEX idx_entries_transaction_id ON entries (transaction_id);

CREATE INDEX idx_entries_account_id ON entries (account_id);

CREATE TABLE
    events (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        transaction_id UUID NOT NULL REFERENCES transactions (id),
        event_type TEXT NOT NULL,
        payload JSONB NOT NULL,
        created_at TIMESTAMPTZ NOT NULL DEFAULT now ()
    );

CREATE INDEX idx_events_transaction_id ON events (transaction_id);

CREATE TABLE
    provider_statement_lines (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        provider_reference TEXT NOT NULL UNIQUE,
        amount BIGINT NOT NULL,
        status TEXT NOT NULL,
        reported_at TIMESTAMPTZ NOT NULL
    );

CREATE TABLE
    discrepancies (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        transaction_id UUID REFERENCES transactions (id),
        provider_reference TEXT,
        discrepancy_type TEXT NOT NULL,
        detected_at TIMESTAMPTZ NOT NULL DEFAULT now (),
        resolved BOOLEAN NOT NULL DEFAULT false
    );

CREATE INDEX idx_discrepancies_transaction_id ON discrepancies (transaction_id);