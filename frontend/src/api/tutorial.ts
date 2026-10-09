import { apiClient } from './client'

export interface Tutorial {
  id: number
  slug: string
  title: string
  summary: string
  category: string
  content_html: string
  content_markdown: string
  status: 'draft' | 'published' | 'offline'
  is_public: boolean
  sort_order: number
  published_at?: string | null
  created_at: string
  updated_at: string
}

export interface TutorialAsset {
  id: number
  tutorial_id?: number | null
  kind: 'image' | 'video' | 'file'
  storage_key: string
  public_url: string
  original_name: string
  label: string
  mime_type: string
  size_bytes: number
  sha256: string
}

export interface TutorialInput {
  slug: string
  title: string
  summary?: string
  category?: string
  content_html: string
  content_markdown?: string
  is_public?: boolean
  sort_order?: number
}

export async function listTutorials(params?: { query?: string; category?: string; page?: number; page_size?: number }) {
  const { data } = await apiClient.get('/tutorials', { params })
  return data as { items: Tutorial[]; total: number; page: number; page_size: number; pages: number }
}

export async function getTutorial(slug: string): Promise<Tutorial> {
  const { data } = await apiClient.get<Tutorial>(`/tutorials/${encodeURIComponent(slug)}`)
  return data
}

export async function listAdminTutorials(params?: { query?: string; category?: string; page?: number; page_size?: number }) {
  const { data } = await apiClient.get('/admin/tutorials', { params })
  return data as { items: Tutorial[]; total: number; page: number; page_size: number; pages: number }
}

export async function createTutorial(input: TutorialInput): Promise<Tutorial> {
  const { data } = await apiClient.post<Tutorial>('/admin/tutorials', input)
  return data
}

export async function updateTutorial(id: number, input: Partial<TutorialInput>): Promise<Tutorial> {
  const { data } = await apiClient.patch<{ tutorial: Tutorial; assets: TutorialAsset[] }>(`/admin/tutorials/${id}`, input)
  return data.tutorial || (data as unknown as Tutorial)
}

export async function getAdminTutorial(id: number): Promise<{ tutorial: Tutorial; assets: TutorialAsset[] }> {
  const { data } = await apiClient.get<{ tutorial: Tutorial; assets: TutorialAsset[] }>(`/admin/tutorials/${id}`)
  return data
}

export async function setTutorialStatus(id: number, status: Tutorial['status']): Promise<void> {
  await apiClient.put(`/admin/tutorials/${id}/status`, { status })
}

export async function deleteTutorial(id: number): Promise<void> {
  await apiClient.delete(`/admin/tutorials/${id}`)
}

export async function getTutorialSupport() {
  const { data } = await apiClient.get('/tutorial-support')
  return data
}
