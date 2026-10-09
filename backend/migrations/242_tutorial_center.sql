-- Tutorial center: isolated content, publication state, and structured resources.
CREATE TABLE IF NOT EXISTS tutorial_documents (
    id BIGSERIAL PRIMARY KEY,
    slug VARCHAR(160) NOT NULL UNIQUE,
    title VARCHAR(160) NOT NULL,
    summary VARCHAR(400) NOT NULL DEFAULT '',
    category VARCHAR(40) NOT NULL DEFAULT 'getting-started',
    content_html TEXT NOT NULL DEFAULT '',
    content_markdown TEXT NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'offline')),
    is_public BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order INTEGER NOT NULL DEFAULT 100,
    published_at TIMESTAMPTZ NULL,
    created_by BIGINT NOT NULL,
    updated_by BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS tutorial_documents_status_sort_idx
    ON tutorial_documents (status, sort_order, id);
CREATE INDEX IF NOT EXISTS tutorial_documents_category_idx
    ON tutorial_documents (category, status, sort_order, id);

CREATE TABLE IF NOT EXISTS tutorial_assets (
    id BIGSERIAL PRIMARY KEY,
    tutorial_id BIGINT NULL REFERENCES tutorial_documents(id) ON DELETE SET NULL,
    kind VARCHAR(20) NOT NULL CHECK (kind IN ('image', 'video', 'file')),
    storage_key TEXT NOT NULL UNIQUE,
    public_url TEXT NOT NULL,
    original_name VARCHAR(255) NOT NULL,
    label VARCHAR(160) NOT NULL DEFAULT '',
    mime_type VARCHAR(120) NOT NULL,
    size_bytes BIGINT NOT NULL,
    sha256 CHAR(64) NOT NULL,
    created_by BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS tutorial_assets_tutorial_idx
    ON tutorial_assets (tutorial_id, id);
