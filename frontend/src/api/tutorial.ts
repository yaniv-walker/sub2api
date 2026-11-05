import { apiClient } from "./client";

export interface TutorialResource {
  label: string;
  url: string;
  version: string;
  platform: string;
  sha256: string;
}
export interface Tutorial {
  id: number;
  slug: string;
  title: string;
  summary: string;
  category: string;
  content_html: string;
  content_markdown: string;
  status: "draft" | "published" | "offline";
  is_public: boolean;
  sort_order: number;
  version: number;
  resources: TutorialResource[];
  source_origin?: string;
  source_document_id?: string;
  source_relation?: string;
  published_at?: string | null;
  created_at: string;
  updated_at: string;
}
export interface TutorialAsset {
  id: number;
  tutorial_id?: number | null;
  kind: "image" | "video" | "file";
  storage_key: string;
  public_url: string;
  preview_url?: string;
  original_name: string;
  label: string;
  mime_type: string;
  size_bytes: number;
  sha256: string;
  references?: { id: number; title: string; slug: string }[];
}
export type TutorialInput = Pick<
  Tutorial,
  | "slug"
  | "title"
  | "summary"
  | "category"
  | "content_markdown"
  | "is_public"
  | "sort_order"
  | "resources"
> & { version?: number; content_html?: string };
export interface TutorialRevision {
  id: number;
  tutorial_id: number;
  version: number;
  snapshot: Tutorial;
  created_at: string;
  created_by: number;
}
export interface TutorialSupport {
  title: string;
  description: string;
  wechat: string;
  wechat_qr_url: string;
  qq: string;
  qq_group: string;
  qq_qr_url: string;
  email: string;
  ticket_url: string;
  service_hours: string;
  public_enabled: boolean;
}
export interface TutorialPage {
  items: Tutorial[];
  total: number;
  page: number;
  page_size: number;
  pages: number;
}
export interface TutorialParams {
  query?: string;
  category?: string;
  page?: number;
  page_size?: number;
}

export async function listTutorials(
  params?: TutorialParams,
  publicly = false,
): Promise<TutorialPage> {
  return (
    await apiClient.get(publicly ? "/public/tutorials" : "/tutorials", {
      params,
    })
  ).data;
}
export async function getTutorial(
  slug: string,
  publicly = false,
): Promise<Tutorial> {
  return (
    await apiClient.get(
      `${publicly ? "/public/tutorials" : "/tutorials"}/${encodeURIComponent(slug)}`,
    )
  ).data;
}
export async function listAdminTutorials(
  params?: TutorialParams,
): Promise<TutorialPage> {
  return (await apiClient.get("/admin/tutorials", { params })).data;
}
export async function createTutorial(input: TutorialInput): Promise<Tutorial> {
  return (await apiClient.post("/admin/tutorials", input)).data;
}
export async function updateTutorial(
  id: number,
  input: Partial<TutorialInput>,
): Promise<Tutorial> {
  const { data } = await apiClient.patch(`/admin/tutorials/${id}`, input);
  return data.tutorial || data;
}
export async function getAdminTutorial(
  id: number,
): Promise<{ tutorial: Tutorial; assets: TutorialAsset[] }> {
  return (await apiClient.get(`/admin/tutorials/${id}`)).data;
}
export async function setTutorialStatus(
  id: number,
  status: Tutorial["status"],
  version: number,
): Promise<Tutorial> {
  return (
    await apiClient.put(`/admin/tutorials/${id}/status`, { status, version })
  ).data;
}
export async function deleteTutorial(id: number, version: number): Promise<void> {
  await apiClient.delete(`/admin/tutorials/${id}`, { params: { version } });
}
export async function listTutorialRevisions(
  id: number,
): Promise<TutorialRevision[]> {
  return (await apiClient.get(`/admin/tutorials/${id}/revisions`)).data;
}
export async function restoreTutorialRevision(
  id: number,
  revisionId: number,
  version: number,
): Promise<Tutorial> {
  const { data } = await apiClient.post(`/admin/tutorials/${id}/restore`, {
    revision_id: revisionId,
    version,
  });
  return data.tutorial || data;
}
export async function listTutorialAssets(id: number): Promise<TutorialAsset[]> {
  return (await apiClient.get(`/admin/tutorial-assets/${id}`)).data;
}
export async function uploadTutorialAsset(
  id: number,
  file: File,
  onProgress?: (value: number) => void,
  assetId?: number,
): Promise<TutorialAsset> {
  const data = new FormData();
  data.append("file", file);
  data.append("label", file.name);
  const url = `/admin/tutorial-assets/${id}${assetId ? `/${assetId}/file` : ""}`;
  return (
    await apiClient.request({
      method: assetId ? "PUT" : "POST",
      url,
      data,
      headers: { "Content-Type": "multipart/form-data" },
      timeout: 600000,
      onUploadProgress: (event) =>
        onProgress?.(
          event.total ? Math.round((event.loaded / event.total) * 100) : 0,
        ),
    })
  ).data;
}
export async function renameTutorialAsset(
  id: number,
  assetId: number,
  label: string,
): Promise<TutorialAsset> {
  return (
    await apiClient.patch(`/admin/tutorial-assets/${id}/${assetId}`, { label })
  ).data;
}
export async function deleteTutorialAsset(
  id: number,
  assetId: number,
): Promise<void> {
  await apiClient.delete(`/admin/tutorial-assets/${id}/${assetId}`);
}
export async function getTutorialSupport(
  admin = false,
  publicly = false,
): Promise<TutorialSupport> {
  return (
    await apiClient.get(admin ? "/admin/tutorial-support" : publicly ? "/public/tutorial-support" : "/tutorial-support")
  ).data;
}
export async function updateTutorialSupport(
  input: TutorialSupport,
): Promise<TutorialSupport> {
  return (await apiClient.put("/admin/tutorial-support", input)).data;
}
