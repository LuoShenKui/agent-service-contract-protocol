-- ASCP 0.2 PostgreSQL persistence blueprint.
--
-- This migration documents durability and uniqueness constraints expected by a
-- production adapter. The Go reference executable remains in-memory. Adapt data
-- types, partitioning, encryption, and row-level security to the deployment.

BEGIN;

CREATE TABLE ascp_offers (
    offer_id             text PRIMARY KEY,
    tenant_id            text NOT NULL,
    actor_type           text NOT NULL,
    actor_id             text NOT NULL,
    principal_type       text NOT NULL,
    principal_id         text NOT NULL,
    negotiation_id       text NOT NULL,
    resolved_intent      text NOT NULL,
    schema_version       text NOT NULL,
    required_scopes      jsonb NOT NULL DEFAULT '[]'::jsonb,
    offer_document       jsonb NOT NULL,
    expires_at           timestamptz NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (jsonb_typeof(required_scopes) = 'array'),
    CHECK (expires_at > created_at)
);

CREATE INDEX ascp_offers_owner_expiry_idx
    ON ascp_offers (tenant_id, actor_id, principal_id, expires_at);

CREATE TABLE ascp_quotes (
    quote_id             text PRIMARY KEY,
    tenant_id            text NOT NULL,
    offer_id             text NOT NULL REFERENCES ascp_offers(offer_id),
    actor_id             text NOT NULL,
    principal_id         text NOT NULL,
    intent               text NOT NULL,
    payload_digest       text NOT NULL UNIQUE,
    quote_document       jsonb NOT NULL,
    issued_at            timestamptz NOT NULL,
    expires_at           timestamptz NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (expires_at > issued_at)
);

CREATE INDEX ascp_quotes_owner_expiry_idx
    ON ascp_quotes (tenant_id, actor_id, principal_id, expires_at);

-- Idempotency state covers Contract mutations, side-effecting Direct calls, and
-- upload-ticket creation. A reconciling claim must not be reused for a new
-- logical request until the original outcome is known.
CREATE TABLE ascp_idempotency (
    tenant_id            text NOT NULL,
    actor_id             text NOT NULL,
    principal_id         text NOT NULL,
    operation            text NOT NULL,
    idempotency_key      text NOT NULL,
    request_digest       text NOT NULL,
    state                text NOT NULL,
    resource_type        text,
    resource_id          text,
    response_status      integer,
    response_headers     jsonb,
    response_body        bytea,
    reconciliation_ref  text,
    original_request_id  text,
    created_at           timestamptz NOT NULL DEFAULT clock_timestamp(),
    completed_at         timestamptz,
    expires_at           timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, actor_id, principal_id, operation, idempotency_key),
    CHECK (length(idempotency_key) BETWEEN 16 AND 255),
    CHECK (state IN ('in_progress', 'completed', 'reconciling')),
    CHECK (
        (state = 'in_progress' AND response_status IS NULL) OR
        (state = 'completed' AND response_status BETWEEN 100 AND 599) OR
        (state = 'reconciling' AND reconciliation_ref IS NOT NULL)
    )
);

CREATE INDEX ascp_idempotency_expiry_idx ON ascp_idempotency (expires_at);
CREATE INDEX ascp_idempotency_reconcile_idx
    ON ascp_idempotency (created_at)
    WHERE state = 'reconciling';

CREATE TABLE ascp_invocations (
    invocation_id        text PRIMARY KEY,
    tenant_id            text NOT NULL,
    actor_id             text NOT NULL,
    principal_id         text NOT NULL,
    intent               text NOT NULL,
    request_digest       text NOT NULL,
    state                text NOT NULL,
    result_document      jsonb NOT NULL,
    receipt_document     jsonb,
    created_at           timestamptz NOT NULL,
    completed_at         timestamptz,
    CHECK (state IN ('accepted', 'succeeded', 'failed'))
);

CREATE INDEX ascp_invocations_owner_time_idx
    ON ascp_invocations (tenant_id, actor_id, principal_id, created_at DESC);

CREATE TABLE ascp_tasks (
    task_id              text PRIMARY KEY,
    tenant_id            text NOT NULL,
    actor_id             text NOT NULL,
    principal_id         text NOT NULL,
    quote_id             text NOT NULL REFERENCES ascp_quotes(quote_id),
    client_task_id       text,
    state                text NOT NULL,
    status_reason        text,
    progress_percent     integer NOT NULL DEFAULT 0,
    version              bigint NOT NULL DEFAULT 1,
    task_document        jsonb NOT NULL,
    created_at           timestamptz NOT NULL,
    updated_at           timestamptz NOT NULL,
    started_at           timestamptz,
    completed_at         timestamptz,
    CHECK (progress_percent BETWEEN 0 AND 100),
    CHECK (version >= 1),
    CHECK (state IN (
        'accepted', 'scheduled', 'running', 'waiting_input', 'waiting_requote',
        'cancelling', 'cancelled', 'succeeded', 'failed', 'compensating',
        'compensated', 'disputed'
    ))
);

CREATE UNIQUE INDEX ascp_tasks_client_id_uq
    ON ascp_tasks (tenant_id, actor_id, principal_id, client_task_id)
    WHERE client_task_id IS NOT NULL;

CREATE INDEX ascp_tasks_worker_idx
    ON ascp_tasks (state, updated_at)
    WHERE state IN ('accepted', 'scheduled', 'running', 'waiting_input', 'compensating');

-- File bytes belong in private object storage. This table stores authoritative
-- metadata and scan state. Upload tokens should be hashed or stored in a
-- dedicated secret/token service rather than plaintext.
CREATE TABLE ascp_files (
    file_id              text PRIMARY KEY,
    tenant_id            text NOT NULL,
    actor_id             text NOT NULL,
    principal_id         text NOT NULL,
    object_key           text NOT NULL UNIQUE,
    name                 text NOT NULL,
    media_type           text NOT NULL,
    size_bytes           bigint NOT NULL,
    sha256_digest        text NOT NULL,
    purpose              text,
    state                text NOT NULL,
    scan_status          text NOT NULL,
    upload_token_hash    bytea,
    upload_expires_at    timestamptz,
    expires_at           timestamptz,
    created_at           timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at           timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (size_bytes >= 0),
    CHECK (state IN ('pending_upload', 'ready', 'rejected', 'expired')),
    CHECK (scan_status IN ('not_scanned', 'pending', 'clean', 'blocked', 'unavailable'))
);

CREATE INDEX ascp_files_owner_state_idx
    ON ascp_files (tenant_id, actor_id, principal_id, state, expires_at);

-- One table can represent pay-now holds, subscription quota, prepaid debits,
-- invoice items, sponsor records, clearing entries, and external settlement.
CREATE TABLE ascp_billing_records (
    billing_record_id    text PRIMARY KEY,
    tenant_id            text NOT NULL,
    task_id              text REFERENCES ascp_tasks(task_id),
    invocation_id        text REFERENCES ascp_invocations(invocation_id),
    quote_id             text REFERENCES ascp_quotes(quote_id),
    mode                  text NOT NULL,
    arrangement_ref      text,
    operation             text NOT NULL,
    provider              text NOT NULL,
    provider_reference   text,
    idempotency_key      text NOT NULL,
    binding_digest       text NOT NULL,
    amount               jsonb,
    invoice_ref          text,
    period_ref           text,
    state                text NOT NULL,
    reconciliation_ref  text,
    error_code           text,
    created_at           timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at           timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (provider, idempotency_key),
    CHECK (mode IN (
        'pay_now', 'prepaid_balance', 'subscription', 'postpaid_account',
        'monthly_invoice', 'clearing_account', 'sponsored', 'external_settlement'
    )),
    CHECK (operation IN ('reserve', 'settle', 'release', 'refund', 'status', 'usage')),
    CHECK ((task_id IS NOT NULL)::integer + (invocation_id IS NOT NULL)::integer <= 1)
);

CREATE INDEX ascp_billing_reconcile_idx
    ON ascp_billing_records (state, updated_at)
    WHERE reconciliation_ref IS NOT NULL OR state IN ('pending', 'unknown', 'retryable_error');

-- Audit chains are generalized to task and invocation resources. Application
-- roles should not receive UPDATE or DELETE privileges on this table.
CREATE TABLE ascp_audit_events (
    resource_type        text NOT NULL,
    resource_id          text NOT NULL,
    sequence             bigint NOT NULL,
    event_id             text NOT NULL UNIQUE,
    event_type           text NOT NULL,
    occurred_at          timestamptz NOT NULL,
    actor_type           text NOT NULL,
    actor_id             text NOT NULL,
    data                  jsonb,
    data_digest           text NOT NULL,
    previous_hash         text,
    event_hash            text NOT NULL UNIQUE,
    signature             jsonb NOT NULL,
    inserted_at           timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (resource_type, resource_id, sequence),
    CHECK (resource_type IN ('task', 'invocation', 'file', 'quote')),
    CHECK (sequence >= 1)
);

CREATE TABLE ascp_receipts (
    receipt_id           text PRIMARY KEY,
    resource_type        text NOT NULL,
    resource_id          text NOT NULL,
    quote_id             text REFERENCES ascp_quotes(quote_id),
    audit_root           text NOT NULL,
    receipt_document     jsonb NOT NULL,
    completed_at         timestamptz NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (resource_type, resource_id),
    CHECK (resource_type IN ('task', 'invocation'))
);

CREATE TABLE ascp_outbox (
    event_id             text PRIMARY KEY,
    aggregate_type       text NOT NULL,
    aggregate_id         text NOT NULL,
    event_type           text NOT NULL,
    payload               jsonb NOT NULL,
    attempts              integer NOT NULL DEFAULT 0,
    available_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    locked_until          timestamptz,
    published_at          timestamptz,
    last_error            text,
    created_at            timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (attempts >= 0)
);

CREATE INDEX ascp_outbox_dispatch_idx
    ON ascp_outbox (available_at, created_at)
    WHERE published_at IS NULL;

COMMIT;
