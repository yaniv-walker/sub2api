import { apiClient } from '../client'

export interface RequestObservabilitySettings {
  enabled: boolean
}

export async function getRequestObservabilitySettings(): Promise<RequestObservabilitySettings> {
  const { data } = await apiClient.get<RequestObservabilitySettings>('/admin/settings/request-observability')
  return data
}

export async function updateRequestObservabilitySettings(enabled: boolean): Promise<RequestObservabilitySettings> {
  const { data } = await apiClient.put<RequestObservabilitySettings>('/admin/settings/request-observability', { enabled })
  return data
}
