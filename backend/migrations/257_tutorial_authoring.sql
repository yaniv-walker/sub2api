-- Tutorial authoring: immutable snapshots, publication isolation and shared assets.
ALTER TABLE tutorial_documents ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1;
ALTER TABLE tutorial_documents ADD COLUMN IF NOT EXISTS resources JSONB NOT NULL DEFAULT '[]';
ALTER TABLE tutorial_documents ADD COLUMN IF NOT EXISTS source_origin TEXT NOT NULL DEFAULT '';
ALTER TABLE tutorial_documents ADD COLUMN IF NOT EXISTS source_document_id TEXT NOT NULL DEFAULT '';
ALTER TABLE tutorial_documents ADD COLUMN IF NOT EXISTS source_updated_at TEXT NOT NULL DEFAULT '';
ALTER TABLE tutorial_documents ADD COLUMN IF NOT EXISTS source_relation TEXT NOT NULL DEFAULT '';
ALTER TABLE tutorial_documents ADD COLUMN IF NOT EXISTS published_snapshot JSONB;
CREATE UNIQUE INDEX IF NOT EXISTS tutorial_documents_source_idx ON tutorial_documents(source_origin,source_document_id) WHERE source_origin <> '' AND source_document_id <> '';
CREATE TABLE IF NOT EXISTS tutorial_revisions (
 id BIGSERIAL PRIMARY KEY,
 tutorial_id BIGINT NOT NULL REFERENCES tutorial_documents(id) ON DELETE CASCADE,
 version BIGINT NOT NULL,
 snapshot JSONB NOT NULL,
 created_by BIGINT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(tutorial_id,version)
);
CREATE TABLE IF NOT EXISTS tutorial_asset_links (
 tutorial_id BIGINT NOT NULL REFERENCES tutorial_documents(id) ON DELETE CASCADE,
 asset_id BIGINT NOT NULL REFERENCES tutorial_assets(id) ON DELETE CASCADE,
 PRIMARY KEY(tutorial_id,asset_id)
);
INSERT INTO tutorial_asset_links(tutorial_id,asset_id) SELECT tutorial_id,id FROM tutorial_assets WHERE tutorial_id IS NOT NULL ON CONFLICT DO NOTHING;
UPDATE tutorial_documents d SET published_snapshot=to_jsonb(d)-'published_snapshot' WHERE status='published' AND published_snapshot IS NULL;
INSERT INTO tutorial_revisions(tutorial_id,version,snapshot,created_by) SELECT id,version,to_jsonb(d)-'published_snapshot',updated_by FROM tutorial_documents d ON CONFLICT DO NOTHING;
