import { apiClient } from '../client'

export interface RequestObservabilitySettings {
  enabled: boolean
}

export async function getRequestObservabilitySettings(): Promise<RequestObservabilitySettings> {
  const { data } = await apiClient.get<RequestObservabilitySettings>('/admin/settings/gateway-remediation')
  return data
}

export async function updateRequestObservabilitySettings(enabled: boolean): Promise<RequestObservabilitySettings> {
  const { data } = await apiClient.put<RequestObservabilitySettings>('/admin/settings/gateway-remediation', { enabled })
  return data
}
